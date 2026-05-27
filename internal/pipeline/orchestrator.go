//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	llmlib "github.com/pgEdge/pgedge-go-llm-lib/llm"

	"github.com/pgEdge/pgedge-rag-server/internal/bm25"
	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/database"
	ragllm "github.com/pgEdge/pgedge-rag-server/internal/llm"
)

// Orchestrator coordinates the RAG pipeline execution.
type Orchestrator struct {
	cfg            *config.Pipeline
	dbPool         *database.Pool
	embeddingProv  Embedder
	completionProv Completer
	bm25Index      *bm25.Index
	tokenBudget    int
	topN           int
	logger         *slog.Logger
}

// OrchestratorConfig contains the configuration for creating an orchestrator.
type OrchestratorConfig struct {
	Pipeline       *config.Pipeline
	DBPool         *database.Pool
	EmbeddingProv  Embedder
	CompletionProv Completer
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
	o.logger.Debug("executing RAG pipeline", "stream", req.Stream, "query_len", len(req.Query))

	topN := o.topN
	if req.TopN > 0 {
		topN = req.TopN
	}

	embedding, err := ragllm.Embed32(ctx, o.embeddingProv, req.Query)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embedding: %w", err)
	}

	results, err := o.search(ctx, req, embedding, topN)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return &QueryResponse{
			Answer:     "No relevant information found in the available documents.",
			TokensUsed: 0,
		}, nil
	}

	contextDocs := o.buildContext(results)

	chatReq := o.buildChatRequest(req, contextDocs)

	resp, err := o.completionProv.Chat(ctx, chatReq)
	if err != nil {
		return nil, fmt.Errorf("failed to generate completion: %w", err)
	}

	answer := joinTextBlocks(resp.Content)

	out := &QueryResponse{
		Answer:     answer,
		TokensUsed: resp.Usage.TotalTokens,
	}
	if req.IncludeSources {
		out.Sources = o.buildSources(results)
	}
	return out, nil
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

		topN := o.topN
		if req.TopN > 0 {
			topN = req.TopN
		}

		embedding, err := ragllm.Embed32(ctx, o.embeddingProv, req.Query)
		if err != nil {
			errChan <- fmt.Errorf("failed to generate embedding: %w", err)
			return
		}

		results, err := o.search(ctx, req, embedding, topN)
		if err != nil {
			errChan <- err
			return
		}

		if len(results) == 0 {
			chunkChan <- StreamChunk{
				Content:      "No relevant information found in the available documents.",
				FinishReason: "stop",
			}
			return
		}

		contextDocs := o.buildContext(results)
		chatReq := o.buildChatRequest(req, contextDocs)

		stream, err := o.completionProv.ChatStream(ctx, chatReq)
		if err != nil {
			errChan <- fmt.Errorf("failed to start completion stream: %w", err)
			return
		}

		for {
			chunk, recvErr := stream.Recv()
			if errors.Is(recvErr, io.EOF) {
				return
			}
			if recvErr != nil {
				errChan <- recvErr
				return
			}

			switch chunk.Type {
			case llmlib.ChunkText:
				if chunk.Text == "" {
					continue
				}
				select {
				case chunkChan <- StreamChunk{Content: chunk.Text}:
				case <-ctx.Done():
					errChan <- ctx.Err()
					return
				}
			case llmlib.ChunkDone:
				// The lib's ChunkDone does not carry a StopReason on
				// the chunk; the pre-migration code emitted "stop" on
				// clean finishes, so we do the same here. If we ever
				// need to surface real stop reasons during streaming,
				// switch to Stream.Collect and read resp.StopReason.
				select {
				case chunkChan <- StreamChunk{FinishReason: "stop"}:
				case <-ctx.Done():
					errChan <- ctx.Err()
					return
				}
			}
		}
	}()

	return chunkChan, errChan
}

// bm25ToSearchResults converts BM25 results into database.SearchResult.
//
// When the table has a configured id_column (hasIDColumn is true), the BM25
// id is a stable identifier shared with the vector arm, so it is preserved:
// both arms then key on the same id in Reciprocal Rank Fusion and a document
// found by both arms fuses into a single entry.
//
// When there is no id_column, the BM25 id is a ROW_NUMBER() assigned
// independently of the vector query and does not identify the same document
// across arms. Carrying it into fusion would leave the two arms in disjoint
// key spaces (BM25 keyed by row number, vector keyed by content), so a shared
// document would appear twice instead of fusing. Clearing the id makes both
// arms key on content — the only reliable cross-arm identity in that case.
func bm25ToSearchResults(
	bm25Results []bm25.SearchResult,
	hasIDColumn bool,
) []database.SearchResult {
	out := make([]database.SearchResult, len(bm25Results))
	for i, r := range bm25Results {
		id := r.ID
		if !hasIDColumn {
			id = ""
		}
		out[i] = database.SearchResult{
			ID:      id,
			Content: r.Content,
			Score:   r.Score,
		}
	}
	return out
}

// search runs the configured vector / hybrid search across all tables
// and returns deduplicated, topN-capped results. Extracted so Execute
// and ExecuteStream share the same retrieval path.
func (o *Orchestrator) search(
	ctx context.Context,
	req QueryRequest,
	embedding []float32,
	topN int,
) ([]database.SearchResult, error) {
	var allResults []database.SearchResult

	vectorWeight := 0.5
	if o.cfg.Search.VectorWeight != nil {
		vectorWeight = *o.cfg.Search.VectorWeight
	}
	if vectorWeight < 0 || vectorWeight > 1 {
		vectorWeight = 0.5
	}

	useHybrid := o.cfg.Search.HybridEnabled != nil && *o.cfg.Search.HybridEnabled &&
		vectorWeight < 1.0

	for _, table := range o.cfg.Tables {
		if o.dbPool == nil {
			o.logger.Warn("no database pool configured", "table", table.Table)
			continue
		}

		vectorResults, err := o.dbPool.VectorSearch(
			ctx, embedding, table, topN*2, req.Filter,
			o.cfg.Search.MinSimilarity,
		)
		if err != nil {
			o.logger.Warn("vector search failed", "table", table.Table, "error", err)
			continue
		}

		if !useHybrid {
			o.logger.Debug("using vector-only search", "table", table.Table)
			allResults = append(allResults, vectorResults...)
			continue
		}

		docs, err := o.dbPool.FetchDocuments(ctx, table, req.Filter)
		if err != nil {
			o.logger.Warn("failed to fetch documents for BM25",
				"table", table.Table, "error", err)
			allResults = append(allResults, vectorResults...)
			continue
		}

		o.bm25Index.Clear()
		o.bm25Index.AddDocuments(docs)
		bm25Results := o.bm25Index.Search(req.Query, topN*2)

		// Clear ids when the table has no stable id_column so fusion
		// keys on content, matching the vector arm.
		bm25SearchResults := bm25ToSearchResults(bm25Results, table.IDColumn != "")

		hybridResults := database.HybridSearch(vectorResults, bm25SearchResults, topN, vectorWeight)
		allResults = append(allResults, hybridResults...)
	}

	return o.deduplicateResults(allResults, topN), nil
}

// buildChatRequest converts the QueryRequest + retrieved context into
// an llmlib.ChatRequest with the system prompt carrying the context
// block. Standardising on system-prompt-carries-context matches the
// pre-migration Anthropic/Gemini behaviour and is functionally
// equivalent for OpenAI/Ollama.
func (o *Orchestrator) buildChatRequest(
	req QueryRequest,
	contextDocs []ragllm.ContextDoc,
) llmlib.ChatRequest {
	system := o.buildSystemPrompt()
	if len(contextDocs) > 0 {
		system = system + "\n\n" + ragllm.FormatContext(contextDocs)
	}

	messages := make([]llmlib.Message, 0, len(req.Messages)+1)
	for _, m := range req.Messages {
		messages = append(messages, llmlib.Message{
			Role: llmlib.Role(m.Role),
			Content: []llmlib.ContentBlock{
				{Type: llmlib.BlockText, Text: m.Content},
			},
		})
	}
	messages = append(messages, llmlib.UserText(req.Query))

	return llmlib.ChatRequest{
		SystemPrompt: system,
		Messages:     messages,
		Temperature:  llmlib.Float(0.7),
	}
}

// joinTextBlocks concatenates the Text fields of all BlockText blocks
// in the response. The lib returns content as a typed slice; today's
// non-RAG API consumers expect a single string in QueryResponse.Answer.
func joinTextBlocks(content []llmlib.ContentBlock) string {
	var sb strings.Builder
	for _, b := range content {
		if b.Type == llmlib.BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
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
func (o *Orchestrator) buildContext(results []database.SearchResult) []ragllm.ContextDoc {
	contextDocs := make([]ragllm.ContextDoc, 0, len(results))
	totalTokens := 0

	for _, r := range results {
		estimatedTokens := len(r.Content) / 4
		if totalTokens+estimatedTokens > o.tokenBudget {
			remaining := o.tokenBudget - totalTokens
			if remaining > 100 {
				truncated := r.Content[:min(len(r.Content), remaining*4)]
				if idx := strings.LastIndex(truncated, ". "); idx > 0 {
					truncated = truncated[:idx+1]
				}
				contextDocs = append(contextDocs, ragllm.ContextDoc{
					Content: truncated + "...",
					Score:   r.Score,
				})
			}
			break
		}

		contextDocs = append(contextDocs, ragllm.ContextDoc{
			Content: r.Content,
			Score:   r.Score,
		})
		totalTokens += estimatedTokens
	}

	return contextDocs
}

// DefaultSystemPrompt is the default system prompt used when none is configured.
const DefaultSystemPrompt = `You are a helpful assistant that answers questions based on the provided context.
Answer the question using ONLY the information from the context.
If the context does not contain relevant information to answer the question, you MUST respond with: "I don't have enough information in the available documents to answer that question."
Do NOT use your general knowledge to answer. Only use facts from the provided context.
Be concise and accurate in your responses.`

// buildSystemPrompt returns the system prompt for RAG.
func (o *Orchestrator) buildSystemPrompt() string {
	if o.cfg != nil && o.cfg.SystemPrompt != "" {
		return o.cfg.SystemPrompt
	}
	return DefaultSystemPrompt
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
