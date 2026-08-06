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
	"github.com/pgEdge/pgedge-rag-server/internal/identity"
	ragllm "github.com/pgEdge/pgedge-rag-server/internal/llm"
)

// Orchestrator coordinates the RAG pipeline execution.
type Orchestrator struct {
	cfg            *config.Pipeline
	dbPool         SearchBackend
	embeddingProv  Embedder
	completionProv Completer
	reranker       Reranker
	rerankTopK     int
	tokenBudget    int
	topN           int
	logger         *slog.Logger
}

// OrchestratorConfig contains the configuration for creating an orchestrator.
type OrchestratorConfig struct {
	Pipeline       *config.Pipeline
	DBPool         SearchBackend
	EmbeddingProv  Embedder
	CompletionProv Completer
	Reranker       Reranker // Optional; nil disables the rerank stage
	RerankTopK     int
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
		reranker:       cfg.Reranker,
		rerankTopK:     cfg.RerankTopK,
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

	results = o.rerank(ctx, req.Query, results)

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
		if o.sourcesAllowed() {
			out.Sources = o.buildSources(results)
		} else {
			o.logger.Warn(
				"include_sources requested but not permitted by pipeline "+
					"configuration; omitting sources",
				"pipeline", o.pipelineName(),
			)
		}
	}
	return out, nil
}

// sourcesAllowed reports whether this pipeline may return the raw
// content of retrieved documents to a client.
//
// Two independent gates must both open before sources are returned: the
// operator permits it here, and the client asks for it via
// include_sources. Keeping them separate means enabling the safety
// control does not force payload on clients that do not want sources,
// and an operator can withdraw permission without waiting on a client
// deploy. A missing config fails closed.
func (o *Orchestrator) sourcesAllowed() bool {
	return o.cfg != nil && o.cfg.AllowIncludeSources
}

// pipelineName is a nil-safe accessor for log messages.
func (o *Orchestrator) pipelineName() string {
	if o.cfg == nil {
		return ""
	}
	return o.cfg.Name
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

		results = o.rerank(ctx, req.Query, results)

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

// errNoSearchBackend is the failure recorded when a pipeline reaches
// the search stage with no database pool. Nothing can be searched, so
// it is a deployment fault of exactly the same kind as a missing grant:
// deterministic, and fixed by whoever configured the pipeline.
var errNoSearchBackend = errors.New("no database pool configured for pipeline")

// retrievalFailure accumulates the failures seen while searching the
// configured tables, keeping the one whose kind has the highest
// precedence (see database.FailureKind) along with its error for the
// operator's log.
type retrievalFailure struct {
	kind database.FailureKind
	err  error
}

// observe records a failed table lookup. The retained error is the one
// matching the winning kind, so the log line accompanying a "refused"
// response describes an actual refusal rather than some other table's
// unrelated timeout.
func (f *retrievalFailure) observe(kind database.FailureKind, err error) {
	if err == nil {
		return
	}
	if f.err == nil || kind > f.kind {
		f.kind = kind
		f.err = err
	}
}

// errorForResultCount decides whether the search as a whole failed.
//
// It distinguishes "the search ran and found nothing" from "the search
// did not run" (issues #25, #49). Any failed table with no results to
// show for the request is a failure: a corpus that is partly unreadable
// is not an empty corpus, and reporting it as one hides a
// misconfiguration behind an answer that looks legitimate. This
// supersedes the partial-failure carve-out from #37, which let a
// refused table pass as an empty result whenever some other table
// happened to search cleanly.
//
// Results in hand still win. A request that retrieved documents can be
// answered, so a table that failed alongside them only narrows
// coverage; that is logged at WARN and does not fail the request.
func (f retrievalFailure) errorForResultCount(resultCount int) error {
	if resultCount > 0 || f.err == nil {
		return nil
	}
	kind := f.kind
	if kind == database.FailureNone {
		kind = database.FailureUnknown
	}
	return &database.RetrievalError{Kind: kind, Err: f.err}
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
//
// If any configured table's search fails and the request ends up with
// no results, a *database.RetrievalError is returned instead of an empty
// slice, so callers can surface an infrastructure failure rather than a
// false "no relevant information" response — see issues #25 and #49. The
// error carries a FailureKind so the API layer can tell a refused query
// apart from an unreachable database. For streaming callers this arrives
// as an "error" SSE event rather than a different HTTP status code,
// since the response status is already committed to 200 by the time
// streaming starts.
func (o *Orchestrator) search(
	ctx context.Context,
	req QueryRequest,
	embedding []float32,
	topN int,
) ([]database.SearchResult, error) {
	var allResults []database.SearchResult
	var failure retrievalFailure

	vectorWeight := 0.5
	if o.cfg.Search.VectorWeight != nil {
		vectorWeight = *o.cfg.Search.VectorWeight
	}
	if vectorWeight < 0 || vectorWeight > 1 {
		vectorWeight = 0.5
	}

	useHybrid := o.cfg.Search.HybridEnabled != nil && *o.cfg.Search.HybridEnabled &&
		vectorWeight < 1.0 && !req.DisableHybrid

	maxBM25Docs := config.DefaultBM25MaxDocuments
	if o.cfg.Search.BM25MaxDocuments != nil && *o.cfg.Search.BM25MaxDocuments > 0 {
		maxBM25Docs = *o.cfg.Search.BM25MaxDocuments
	}

	for _, table := range o.cfg.Tables {
		if o.dbPool == nil {
			o.logger.Warn("no database pool configured", "table", table.Table)
			// A missing pool means this table cannot be searched at all,
			// which is an infrastructure failure rather than a legitimate
			// empty result — mark it so an absence of a usable pool
			// surfaces as an error instead of a false "no relevant
			// information" response (issue #25).
			failure.observe(database.FailureRefused, errNoSearchBackend)
			continue
		}

		vectorResults, err := o.dbPool.VectorSearch(
			ctx, embedding, table, topN*2, req.Filter,
			o.cfg.Search.MinSimilarity,
		)
		if err != nil {
			// A missing identity is not a per-table retrieval failure to
			// be logged and worked around: no table can be searched, and
			// degrading to "no relevant information found" would present
			// a refusal as an empty corpus. Return it so the HTTP layer
			// can say plainly that the request was refused for want of an
			// identity, rather than classifying and accumulating it below.
			if errors.Is(err, identity.ErrRequired) {
				return nil, err
			}
			// The full error, including SQLSTATE and table name, goes to
			// the operator's log; only the kind travels any further
			// towards the caller (issue #49).
			kind := database.ClassifyFailure(err)
			o.logger.Warn("vector search failed",
				"table", table.Table, "failure_kind", kind.String(), "error", err)
			failure.observe(kind, err)
			continue
		}

		if !useHybrid {
			o.logger.Debug("using vector-only search", "table", table.Table)
			allResults = append(allResults, vectorResults...)
			continue
		}

		docs, err := o.dbPool.FetchDocuments(ctx, table, req.Filter, maxBM25Docs)
		if err != nil {
			if errors.Is(err, identity.ErrRequired) {
				return nil, err
			}
			// This counts as a failed table even though the vector arm
			// succeeded, because on a hybrid pipeline the keyword arm is
			// half of the search: when it cannot read the corpus, no
			// keyword matching happened at all for this request. If the
			// vector arm also matched nothing, the request has not
			// established that the corpus holds nothing relevant, only
			// that half a search found nothing — which is the very
			// ambiguity issue #49 is about. It is only decisive when the
			// request ends with no results whatsoever; a vector hit here
			// still answers, with the narrowed coverage left to this log
			// line, as with a corpus truncated by bm25_max_documents.
			kind := database.ClassifyFailure(err)
			o.logger.Warn("failed to fetch documents for BM25; "+
				"no keyword matching ran for this table",
				"table", table.Table, "failure_kind", kind.String(), "error", err)
			failure.observe(kind, err)
			allResults = append(allResults, vectorResults...)
			continue
		}

		if len(docs) >= maxBM25Docs {
			o.logger.Warn(
				"keyword search corpus truncated by bm25_max_documents; "+
					"keyword coverage for this request is partial",
				"table", table.Table, "limit", maxBM25Docs,
			)
		}

		// The index is built per request and never shared. A single
		// per-pipeline index mutated in place (clear, refill, search)
		// cannot be made correct by locking each step, because the three
		// steps are only meaningful as one unit: concurrent requests
		// interleave, so a search can run against a corpus another
		// request's filter populated. Since the filter is how callers
		// scope results, that turns ordinary concurrent traffic into a
		// way to defeat that scoping. Holding one lock across all three
		// steps would fix correctness but serialise every request on the
		// pipeline, which makes the cost problem above worse; a
		// per-request index has neither drawback.
		index := bm25.NewIndex()
		index.AddDocuments(docs)
		bm25Results := index.Search(req.Query, topN*2)

		// Clear ids when the table has no stable id_column so fusion
		// keys on content, matching the vector arm.
		bm25SearchResults := bm25ToSearchResults(bm25Results, table.IDColumn != "")

		hybridResults := database.HybridSearch(vectorResults, bm25SearchResults, topN, vectorWeight)
		allResults = append(allResults, hybridResults...)
	}

	if err := failure.errorForResultCount(len(allResults)); err != nil {
		return nil, err
	}

	return o.deduplicateResults(allResults, topN), nil
}

// rerank reorders results by relevance to the query using the
// configured reranking provider, if any (issue #22). A nil reranker or
// an empty result set is a no-op. A reranking failure only degrades
// ordering — the underlying retrieval already succeeded — so it is
// logged and the original results are returned unchanged rather than
// failing the whole request.
func (o *Orchestrator) rerank(
	ctx context.Context,
	query string,
	results []database.SearchResult,
) []database.SearchResult {
	if o.reranker == nil || len(results) == 0 {
		return results
	}

	docs := make([]string, len(results))
	for i, r := range results {
		docs[i] = r.Content
	}

	var topK *int
	if k := o.rerankTopK; k > 0 && k < len(results) {
		topK = &k
	}

	resp, err := o.reranker.Rerank(ctx, llmlib.RerankRequest{
		Query:     query,
		Documents: docs,
		TopK:      topK,
	})
	if err != nil {
		o.logger.Warn("rerank failed, falling back to original order", "error", err)
		return results
	}

	reranked := o.applyRerankOrder(results, resp.Results)

	// A successful call can still yield nothing usable — an empty
	// response, or every index out of range. Returning that empty slice
	// would drop all context and leave the LLM with nothing to ground
	// on, which is strictly worse than not reranking. A rerank problem
	// should only degrade ordering, never empty the query, so fall back
	// to the original order in that case.
	if len(reranked) == 0 {
		o.logger.Warn("rerank returned no usable results, falling back to original order")
		return results
	}
	return reranked
}

// applyRerankOrder maps a provider's rerank results back onto the
// original retrieval slice: it reorders by the provider's judgment and
// promotes each surviving result's Score to the reranker's relevance
// score. Indices outside the original slice (from a malformed or buggy
// provider response) are logged and skipped rather than panicking.
func (o *Orchestrator) applyRerankOrder(
	results []database.SearchResult,
	rerankResults []llmlib.RerankResult,
) []database.SearchResult {
	reranked := make([]database.SearchResult, 0, len(rerankResults))
	for _, res := range rerankResults {
		if res.Index < 0 || res.Index >= len(results) {
			o.logger.Warn("rerank result index out of range, skipping", "index", res.Index)
			continue
		}
		// Score is documented (OpenAPI) as "relevance score"; once the
		// reranker has judged relevance, its score is what that field
		// means going forward. Leaving the original retrieval score in
		// place would show API consumers a "sources" list that looks
		// unsorted by its own score field, since order now reflects the
		// reranker's judgment rather than the original one.
		promoted := results[res.Index]
		promoted.Score = res.RelevanceScore
		reranked = append(reranked, promoted)
	}
	return reranked
}

// buildChatRequest converts the QueryRequest + retrieved context into
// an llmlib.ChatRequest.
//
// Retrieved content is placed in the final user turn, delimited by a
// per-request nonce, and never in the system prompt. Every provider
// treats the system prompt as the top of its instruction hierarchy, so
// it is the worst available position for bytes an attacker may control:
// a poisoned document concatenated there is presented to the model with
// operator authority. The system prompt instead carries only trusted
// text, including the BoundaryRules that name this request's markers.
//
// This reverses an earlier decision to standardise on
// system-prompt-carries-context. That standardisation was about
// cross-provider consistency rather than security, and it is preserved
// here: the placement changes uniformly for every provider, so
// Anthropic, Gemini, OpenAI and Ollama all continue to receive the same
// structure as each other.
//
// Temperature is intentionally left unset here: pgedge-go-llm-lib's
// Options.WithDefaults() always fills an unset per-request Temperature
// with a client-level default (0.7), so no pgedge-rag-server-side value
// (including omitting it, as here) prevents a temperature field from
// reaching the wire. Some newer models (observed: claude-sonnet-5)
// reject any temperature value outright ("400: `temperature` is
// deprecated for this model"). This is a pgedge-go-llm-lib limitation,
// not something fixable from this layer without hand-rolling
// provider-specific HTTP handling — tracked upstream instead of worked
// around here.
func (o *Orchestrator) buildChatRequest(
	req QueryRequest,
	contextDocs []ragllm.ContextDoc,
) llmlib.ChatRequest {
	system := o.buildSystemPrompt()

	messages := make([]llmlib.Message, 0, len(req.Messages)+1)
	for _, m := range req.Messages {
		messages = append(messages, llmlib.Message{
			Role: llmlib.Role(m.Role),
			Content: []llmlib.ContentBlock{
				{Type: llmlib.BlockText, Text: m.Content},
			},
		})
	}

	if len(contextDocs) > 0 {
		block := ragllm.FormatContext(contextDocs)
		system = system + "\n\n" + ragllm.BoundaryRules(block.Nonce)
		// Context and question share one user turn rather than being
		// sent as two, because consecutive same-role messages are
		// rejected or silently merged by some providers.
		messages = append(messages, llmlib.UserText(
			block.Text+"\n\nQuestion: "+req.Query,
		))
	} else {
		messages = append(messages, llmlib.UserText(req.Query))
	}

	return llmlib.ChatRequest{
		SystemPrompt: system,
		Messages:     messages,
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
					Source:  r.ID,
					Score:   r.Score,
				})
			}
			break
		}

		contextDocs = append(contextDocs, ragllm.ContextDoc{
			Content: r.Content,
			Source:  r.ID,
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

// buildSystemPrompt returns the operator-controlled part of the system
// prompt: a persona and answering style, either the configured
// system_prompt or DefaultSystemPrompt.
//
// This is only part of what the model receives. buildChatRequest
// appends ragllm.BoundaryRules beneath whatever this returns whenever
// there is retrieved content, so a custom system_prompt replaces the
// persona but cannot displace the trust boundary. Note that the
// anti-hallucination wording in DefaultSystemPrompt is topicality
// guidance, not a security control: it constrains where facts come
// from and does nothing to stop a document issuing instructions.
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
