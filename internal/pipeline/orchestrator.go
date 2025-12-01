//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pgEdge/pgedge-rag-server/internal/bm25"
	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/database"
	"github.com/pgEdge/pgedge-rag-server/internal/llm"
)

// Orchestrator coordinates the RAG pipeline execution.
type Orchestrator struct {
	cfg            *config.Pipeline
	dbPool         *database.Pool
	embeddingProv  llm.EmbeddingProvider
	completionProv llm.CompletionProvider
	bm25Index      *bm25.Index
	tokenBudget    int
	topN           int
	logger         *slog.Logger
}

// OrchestratorConfig contains the configuration for creating an orchestrator.
type OrchestratorConfig struct {
	Pipeline       *config.Pipeline
	DBPool         *database.Pool
	EmbeddingProv  llm.EmbeddingProvider
	CompletionProv llm.CompletionProvider
	TokenBudget    int
	TopN           int
	Logger         *slog.Logger
}

// NewOrchestrator creates a new RAG pipeline orchestrator.
func NewOrchestrator(cfg OrchestratorConfig) *Orchestrator {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Orchestrator{
		cfg:            cfg.Pipeline,
		dbPool:         cfg.DBPool,
		embeddingProv:  cfg.EmbeddingProv,
		completionProv: cfg.CompletionProv,
		bm25Index:      bm25.NewIndex(),
		tokenBudget:    cfg.TokenBudget,
		topN:           cfg.TopN,
		logger:         logger,
	}
}

// Execute runs the full RAG pipeline for a query.
func (o *Orchestrator) Execute(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
	o.logger.Debug("executing RAG pipeline",
		"query", req.Query,
		"stream", req.Stream,
	)

	// Get topN from request or use default
	topN := o.topN
	if req.TopN > 0 {
		topN = req.TopN
	}

	// Step 1: Generate query embedding
	embedding, err := o.embeddingProv.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding: %w", err)
	}

	// Step 2: Perform hybrid search for each column pair
	var allResults []database.SearchResult

	for _, columnPair := range o.cfg.ColumnPairs {
		// Skip if no database pool (shouldn't happen in production)
		if o.dbPool == nil {
			o.logger.Warn("no database pool configured",
				"table", columnPair.Table,
			)
			continue
		}

		// Vector search (with optional filter from request)
		vectorResults, err := o.dbPool.VectorSearch(ctx, embedding, columnPair, topN*2, req.Filter)
		if err != nil {
			o.logger.Warn("vector search failed",
				"table", columnPair.Table,
				"error", err,
			)
			continue
		}

		// BM25 search - need to fetch documents first and index them
		docs, err := o.dbPool.FetchDocuments(ctx, columnPair, req.Filter)
		if err != nil {
			o.logger.Warn("failed to fetch documents for BM25",
				"table", columnPair.Table,
				"error", err,
			)
			// Continue with just vector results
			allResults = append(allResults, vectorResults...)
			continue
		}

		// Index documents for BM25
		o.bm25Index.Clear()
		o.bm25Index.AddDocuments(docs)

		// Search with BM25
		bm25Results := o.bm25Index.Search(req.Query, topN*2)

		// Convert BM25 results to SearchResult format
		bm25SearchResults := make([]database.SearchResult, len(bm25Results))
		for i, r := range bm25Results {
			bm25SearchResults[i] = database.SearchResult{
				ID:      r.ID,
				Content: r.Content,
				Score:   r.Score,
			}
		}

		// Combine using RRF
		hybridResults := database.HybridSearch(vectorResults, bm25SearchResults, topN)
		allResults = append(allResults, hybridResults...)
	}

	// Step 3: Deduplicate and limit results
	results := o.deduplicateResults(allResults, topN)

	// Check if we have any results - if not, return an error
	if len(results) == 0 {
		return nil, fmt.Errorf("no documents found for query")
	}

	// Step 4: Build context with token budget
	contextDocs := o.buildContext(results)

	// Step 5: Generate completion
	// Build messages array with conversation history
	messages := make([]llm.Message, 0, len(req.Messages)+1)
	for _, m := range req.Messages {
		messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
	}
	messages = append(messages, llm.Message{Role: "user", Content: req.Query})

	completionReq := llm.CompletionRequest{
		SystemPrompt: o.buildSystemPrompt(),
		Context:      contextDocs,
		Messages:     messages,
		Temperature:  0.7,
	}

	// Non-streaming response
	completionResp, err := o.completionProv.Complete(ctx, completionReq)
	if err != nil {
		return nil, fmt.Errorf("failed to generate completion: %w", err)
	}

	resp := &QueryResponse{
		Answer:     completionResp.Content,
		TokensUsed: completionResp.Usage.TotalTokens,
	}

	// Only include sources if requested
	if req.IncludeSources {
		resp.Sources = o.buildSources(results)
	}

	return resp, nil
}

// ExecuteStream runs the RAG pipeline and returns a streaming response.
func (o *Orchestrator) ExecuteStream(
	ctx context.Context,
	req QueryRequest,
) (<-chan StreamChunk, <-chan error) {
	chunkChan := make(chan StreamChunk)
	errChan := make(chan error, 1)

	go func() {
		defer close(chunkChan)
		defer close(errChan)

		// Get topN from request or use default
		topN := o.topN
		if req.TopN > 0 {
			topN = req.TopN
		}

		// Step 1: Generate query embedding
		embedding, err := o.embeddingProv.Embed(ctx, req.Query)
		if err != nil {
			errChan <- fmt.Errorf("failed to generate embedding: %w", err)
			return
		}

		// Step 2: Perform hybrid search
		var allResults []database.SearchResult

		for _, columnPair := range o.cfg.ColumnPairs {
			// Skip if no database pool (shouldn't happen in production)
			if o.dbPool == nil {
				o.logger.Warn("no database pool configured",
					"table", columnPair.Table,
				)
				continue
			}

			vectorResults, err := o.dbPool.VectorSearch(ctx, embedding, columnPair, topN*2, req.Filter)
			if err != nil {
				o.logger.Warn("vector search failed", "error", err)
				continue
			}

			docs, err := o.dbPool.FetchDocuments(ctx, columnPair, req.Filter)
			if err != nil {
				allResults = append(allResults, vectorResults...)
				continue
			}

			o.bm25Index.Clear()
			o.bm25Index.AddDocuments(docs)

			bm25Results := o.bm25Index.Search(req.Query, topN*2)
			bm25SearchResults := make([]database.SearchResult, len(bm25Results))
			for i, r := range bm25Results {
				bm25SearchResults[i] = database.SearchResult{
					ID:      r.ID,
					Content: r.Content,
					Score:   r.Score,
				}
			}

			hybridResults := database.HybridSearch(vectorResults, bm25SearchResults, topN)
			allResults = append(allResults, hybridResults...)
		}

		results := o.deduplicateResults(allResults, topN)

		// Check if we have any results - if not, return an error
		if len(results) == 0 {
			errChan <- fmt.Errorf("no documents found for query")
			return
		}

		contextDocs := o.buildContext(results)

		// Step 3: Stream completion
		// Build messages array with conversation history
		messages := make([]llm.Message, 0, len(req.Messages)+1)
		for _, m := range req.Messages {
			messages = append(messages, llm.Message{Role: m.Role, Content: m.Content})
		}
		messages = append(messages, llm.Message{Role: "user", Content: req.Query})

		completionReq := llm.CompletionRequest{
			SystemPrompt: o.buildSystemPrompt(),
			Context:      contextDocs,
			Messages:     messages,
			Temperature:  0.7,
		}

		llmChunkChan, llmErrChan := o.completionProv.CompleteStream(ctx, completionReq)

		// Forward chunks
		for chunk := range llmChunkChan {
			select {
			case chunkChan <- StreamChunk{
				Content:      chunk.Content,
				FinishReason: chunk.FinishReason,
			}:
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			}
		}

		// Check for errors
		if err := <-llmErrChan; err != nil {
			errChan <- err
		}
	}()

	return chunkChan, errChan
}

// deduplicateResults removes duplicate content and limits to topN.
func (o *Orchestrator) deduplicateResults(
	results []database.SearchResult,
	topN int,
) []database.SearchResult {
	seen := make(map[string]bool)
	unique := make([]database.SearchResult, 0, min(len(results), topN))

	for _, r := range results {
		key := r.Content
		if r.ID != "" {
			key = r.ID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, r)
		if len(unique) >= topN {
			break
		}
	}

	return unique
}

// buildContext converts search results to context documents, respecting token budget.
func (o *Orchestrator) buildContext(results []database.SearchResult) []llm.ContextDocument {
	contextDocs := make([]llm.ContextDocument, 0, len(results))
	totalTokens := 0

	for _, r := range results {
		// Rough token estimate: ~4 characters per token
		estimatedTokens := len(r.Content) / 4
		if totalTokens+estimatedTokens > o.tokenBudget {
			// Truncate content to fit within budget
			remaining := o.tokenBudget - totalTokens
			if remaining > 100 {
				truncated := r.Content[:min(len(r.Content), remaining*4)]
				if idx := strings.LastIndex(truncated, ". "); idx > 0 {
					truncated = truncated[:idx+1]
				}
				contextDocs = append(contextDocs, llm.ContextDocument{
					Content: truncated + "...",
					Score:   r.Score,
				})
			}
			break
		}

		contextDocs = append(contextDocs, llm.ContextDocument{
			Content: r.Content,
			Score:   r.Score,
		})
		totalTokens += estimatedTokens
	}

	return contextDocs
}

// buildSystemPrompt returns the system prompt for RAG.
func (o *Orchestrator) buildSystemPrompt() string {
	return `You are a helpful assistant that answers questions based on the provided context.
Answer the question using only the information from the context.
If the context doesn't contain enough information to answer, say so.
Be concise and accurate in your responses.`
}

// buildSources extracts source information from results.
func (o *Orchestrator) buildSources(results []database.SearchResult) []Source {
	sources := make([]Source, len(results))
	for i, r := range results {
		sources[i] = Source{
			ID:      r.ID,
			Content: r.Content,
			Score:   r.Score,
		}
	}
	return sources
}
