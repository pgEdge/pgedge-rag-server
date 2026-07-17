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
	"io"
	"strings"
	"testing"

	llmlib "github.com/pgEdge/pgedge-go-llm-lib/llm"

	"github.com/pgEdge/pgedge-rag-server/internal/bm25"
	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/database"
)

// MockEmbedder implements pipeline.Embedder for orchestrator tests.
type MockEmbedder struct {
	EmbedFunc func(ctx context.Context, text string) ([]float64, error)
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if m.EmbedFunc != nil {
		return m.EmbedFunc(ctx, text)
	}
	return []float64{0.1, 0.2, 0.3}, nil
}

// MockCompleter implements pipeline.Completer for orchestrator tests.
type MockCompleter struct {
	ChatFunc       func(ctx context.Context, req llmlib.ChatRequest) (*llmlib.ChatResponse, error)
	ChatStreamFunc func(ctx context.Context, req llmlib.ChatRequest) (*llmlib.Stream, error)
}

func (m *MockCompleter) Chat(
	ctx context.Context,
	req llmlib.ChatRequest,
) (*llmlib.ChatResponse, error) {
	if m.ChatFunc != nil {
		return m.ChatFunc(ctx, req)
	}
	return &llmlib.ChatResponse{
		Content: []llmlib.ContentBlock{
			{Type: llmlib.BlockText, Text: "This is a mock response."},
		},
		StopReason: llmlib.StopReasonEndTurn,
		Usage:      llmlib.TokenUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
	}, nil
}

func (m *MockCompleter) ChatStream(
	ctx context.Context,
	req llmlib.ChatRequest,
) (*llmlib.Stream, error) {
	if m.ChatStreamFunc != nil {
		return m.ChatStreamFunc(ctx, req)
	}

	chunks := make(chan llmlib.StreamChunk, 4)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)
		chunks <- llmlib.StreamChunk{Type: llmlib.ChunkText, Text: "This is "}
		chunks <- llmlib.StreamChunk{Type: llmlib.ChunkText, Text: "a streaming response."}
		chunks <- llmlib.StreamChunk{
			Type:  llmlib.ChunkDone,
			Usage: &llmlib.TokenUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
		}
	}()

	return &llmlib.Stream{Chunks: chunks, Err: errs}, nil
}

func TestNewOrchestrator(t *testing.T) {
	cfg := OrchestratorConfig{
		Pipeline: &config.Pipeline{
			Name: "test-pipeline",
		},
		EmbeddingProv:  &MockEmbedder{},
		CompletionProv: &MockCompleter{},
		TokenBudget:    4000,
		TopN:           5,
	}

	orch := NewOrchestrator(cfg)

	if orch == nil {
		t.Fatal("NewOrchestrator returned nil")
	}
	if orch.tokenBudget != 4000 {
		t.Errorf("expected tokenBudget 4000, got %d", orch.tokenBudget)
	}
	if orch.topN != 5 {
		t.Errorf("expected topN 5, got %d", orch.topN)
	}
	if orch.bm25Index == nil {
		t.Error("bm25Index should not be nil")
	}
	if orch.logger == nil {
		t.Error("logger should not be nil")
	}
}

func TestDeduplicateResults(t *testing.T) {
	orch := &Orchestrator{
		bm25Index: bm25.NewIndex(),
	}

	tests := []struct {
		name     string
		results  []database.SearchResult
		topN     int
		expected int
	}{
		{
			name: "no duplicates",
			results: []database.SearchResult{
				{ID: "1", Content: "doc1", Score: 0.9},
				{ID: "2", Content: "doc2", Score: 0.8},
				{ID: "3", Content: "doc3", Score: 0.7},
			},
			topN:     5,
			expected: 3,
		},
		{
			name: "with duplicates by ID",
			results: []database.SearchResult{
				{ID: "1", Content: "doc1", Score: 0.9},
				{ID: "1", Content: "doc1", Score: 0.85},
				{ID: "2", Content: "doc2", Score: 0.8},
			},
			topN:     5,
			expected: 2,
		},
		{
			name: "with duplicates by content",
			results: []database.SearchResult{
				{Content: "same content", Score: 0.9},
				{Content: "same content", Score: 0.85},
				{Content: "different", Score: 0.8},
			},
			topN:     5,
			expected: 2,
		},
		{
			name: "limit to topN",
			results: []database.SearchResult{
				{ID: "1", Content: "doc1", Score: 0.9},
				{ID: "2", Content: "doc2", Score: 0.8},
				{ID: "3", Content: "doc3", Score: 0.7},
				{ID: "4", Content: "doc4", Score: 0.6},
			},
			topN:     2,
			expected: 2,
		},
		{
			name:     "empty results",
			results:  []database.SearchResult{},
			topN:     5,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := orch.deduplicateResults(tt.results, tt.topN)
			if len(result) != tt.expected {
				t.Errorf("expected %d results, got %d", tt.expected, len(result))
			}
		})
	}
}

func TestBuildContext(t *testing.T) {
	tests := []struct {
		name        string
		tokenBudget int
		results     []database.SearchResult
		expectCount int
		expectTrunc bool
	}{
		{
			name:        "all results fit",
			tokenBudget: 1000,
			results: []database.SearchResult{
				{Content: "Short content 1", Score: 0.9},
				{Content: "Short content 2", Score: 0.8},
			},
			expectCount: 2,
			expectTrunc: false,
		},
		{
			name:        "truncation needed",
			tokenBudget: 150, // Budget allows first doc truncated, not second
			results: []database.SearchResult{
				{Content: "This is the first document with enough content. " +
					"It needs to be long enough that the second document causes truncation. " +
					"Adding more text here to pad out the content for testing purposes. " +
					"We want this to fit but leave little room for the next one.", Score: 0.9},
				{Content: "Second document with a lot of content that should trigger " +
					"truncation because we're nearing the token budget limit. " +
					"This content should be partially included with an ellipsis.", Score: 0.8},
			},
			expectCount: 2,
			expectTrunc: true,
		},
		{
			name:        "empty results",
			tokenBudget: 1000,
			results:     []database.SearchResult{},
			expectCount: 0,
			expectTrunc: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orch := &Orchestrator{
				tokenBudget: tt.tokenBudget,
				bm25Index:   bm25.NewIndex(),
			}

			contextDocs := orch.buildContext(tt.results)

			if len(contextDocs) != tt.expectCount {
				t.Errorf("expected %d context docs, got %d", tt.expectCount, len(contextDocs))
			}

			if tt.expectTrunc && len(contextDocs) > 0 {
				lastDoc := contextDocs[len(contextDocs)-1]
				if len(lastDoc.Content) >= len(tt.results[0].Content) {
					t.Error("expected content to be truncated")
				}
			}
		})
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	orch := &Orchestrator{
		bm25Index: bm25.NewIndex(),
	}

	prompt := orch.buildSystemPrompt()

	if prompt == "" {
		t.Error("system prompt should not be empty")
	}

	// Verify it contains expected phrases
	expectedPhrases := []string{
		"helpful assistant",
		"context",
		"answer",
	}

	for _, phrase := range expectedPhrases {
		if !containsPhrase(prompt, phrase) {
			t.Errorf("system prompt should contain '%s'", phrase)
		}
	}
}

func TestBuildSystemPrompt_CustomPrompt(t *testing.T) {
	customPrompt := "You are Ellie, a custom assistant for pgEdge docs."

	orch := &Orchestrator{
		cfg: &config.Pipeline{
			Name:         "test-pipeline",
			SystemPrompt: customPrompt,
		},
		bm25Index: bm25.NewIndex(),
	}

	prompt := orch.buildSystemPrompt()

	if prompt != customPrompt {
		t.Errorf("expected custom prompt %q, got %q", customPrompt, prompt)
	}
}

func TestBuildSystemPrompt_EmptyConfigPrompt(t *testing.T) {
	// When SystemPrompt is empty string, should fall back to default
	orch := &Orchestrator{
		cfg: &config.Pipeline{
			Name:         "test-pipeline",
			SystemPrompt: "", // Empty
		},
		bm25Index: bm25.NewIndex(),
	}

	prompt := orch.buildSystemPrompt()

	if prompt != DefaultSystemPrompt {
		t.Errorf("expected default prompt when config has empty SystemPrompt")
	}
}

func TestSystemPromptPassedToCompletion(t *testing.T) {
	// This test verifies that the custom system prompt is correctly
	// configured in the orchestrator and would be passed to completion
	customPrompt := "You are Ellie, a custom assistant."

	orch := NewOrchestrator(OrchestratorConfig{
		Pipeline: &config.Pipeline{
			Name:         "test-pipeline",
			SystemPrompt: customPrompt,
			Tables: []config.TableSource{
				{Table: "docs", TextColumn: "content", VectorColumn: "embedding"},
			},
		},
		EmbeddingProv:  &MockEmbedder{},
		CompletionProv: &MockCompleter{},
		TokenBudget:    4000,
		TopN:           5,
	})

	// Verify the orchestrator's buildSystemPrompt returns the custom prompt
	builtPrompt := orch.buildSystemPrompt()
	if builtPrompt != customPrompt {
		t.Errorf("buildSystemPrompt() = %q, want %q", builtPrompt, customPrompt)
	}
}

func containsPhrase(s, phrase string) bool {
	return strings.Contains(s, phrase)
}

func TestBuildSources(t *testing.T) {
	orch := &Orchestrator{
		bm25Index: bm25.NewIndex(),
	}

	results := []database.SearchResult{
		{ID: "doc1", Content: "Content 1", Score: 0.95},
		{ID: "doc2", Content: "Content 2", Score: 0.85},
		{ID: "", Content: "Content 3", Score: 0.75},
	}

	sources := orch.buildSources(results)

	if len(sources) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(sources))
	}

	// Verify first source
	if sources[0].ID != "doc1" {
		t.Errorf("expected ID 'doc1', got '%s'", sources[0].ID)
	}
	if sources[0].Content != "Content 1" {
		t.Errorf("expected Content 'Content 1', got '%s'", sources[0].Content)
	}
	if sources[0].Score != 0.95 {
		t.Errorf("expected Score 0.95, got %f", sources[0].Score)
	}

	// Verify empty ID is preserved
	if sources[2].ID != "" {
		t.Errorf("expected empty ID, got '%s'", sources[2].ID)
	}
}

func TestQueryRequestTopNOverride(t *testing.T) {
	// Test that request-level TopN overrides orchestrator default
	orch := &Orchestrator{
		topN:      10, // Default
		bm25Index: bm25.NewIndex(),
	}

	// Simulate getting topN from request
	req := QueryRequest{
		Query: "test query",
		TopN:  5, // Override
	}

	topN := orch.topN
	if req.TopN > 0 {
		topN = req.TopN
	}

	if topN != 5 {
		t.Errorf("expected topN to be 5, got %d", topN)
	}

	// Test no override
	req2 := QueryRequest{
		Query: "test query",
	}

	topN2 := orch.topN
	if req2.TopN > 0 {
		topN2 = req2.TopN
	}

	if topN2 != 10 {
		t.Errorf("expected topN to be 10, got %d", topN2)
	}
}

// Test mock embedder/completer work correctly
func TestMockEmbedder(t *testing.T) {
	mb := &MockEmbedder{}
	v, err := mb.Embed(context.Background(), "x")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(v) != 3 {
		t.Errorf("expected 3 dims, got %d", len(v))
	}
}

func TestMockCompleter_Chat(t *testing.T) {
	mc := &MockCompleter{}
	resp, err := mc.Chat(context.Background(), llmlib.ChatRequest{
		SystemPrompt: "You are a test assistant.",
		Messages:     []llmlib.Message{llmlib.UserText("Hello")},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if len(resp.Content) == 0 || resp.Content[0].Text != "This is a mock response." {
		t.Errorf("unexpected response content: %+v", resp.Content)
	}
}

func TestMockCompleter_ChatStream(t *testing.T) {
	mc := &MockCompleter{}
	stream, err := mc.ChatStream(context.Background(), llmlib.ChatRequest{
		Messages: []llmlib.Message{llmlib.UserText("Hello")},
	})
	if err != nil {
		t.Fatalf("ChatStream failed: %v", err)
	}

	var body strings.Builder
	for {
		chunk, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			t.Fatalf("Recv: %v", recvErr)
		}
		if chunk.Type == llmlib.ChunkText {
			body.WriteString(chunk.Text)
		}
	}
	if body.String() != "This is a streaming response." {
		t.Errorf("unexpected streaming body: %q", body.String())
	}
}

func TestMockCompleter_CustomChatFunc(t *testing.T) {
	mc := &MockCompleter{
		ChatFunc: func(ctx context.Context, req llmlib.ChatRequest) (*llmlib.ChatResponse, error) {
			return &llmlib.ChatResponse{
				Content: []llmlib.ContentBlock{
					{Type: llmlib.BlockText, Text: "Custom: " + req.Messages[0].Content[0].Text},
				},
			}, nil
		},
	}
	resp, err := mc.Chat(context.Background(), llmlib.ChatRequest{
		Messages: []llmlib.Message{llmlib.UserText("ping")},
	})
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if resp.Content[0].Text != "Custom: ping" {
		t.Errorf("unexpected content: %+v", resp.Content)
	}
}

func TestMockEmbedder_CustomErrorFunc(t *testing.T) {
	mb := &MockEmbedder{
		EmbedFunc: func(ctx context.Context, text string) ([]float64, error) {
			return nil, errors.New("embedding error")
		},
	}
	if _, err := mb.Embed(context.Background(), "x"); err == nil || err.Error() != "embedding error" {
		t.Errorf("expected 'embedding error', got %v", err)
	}
}

func TestBuildSystemPrompt_DefaultContainsAntiHallucination(t *testing.T) {
	orch := &Orchestrator{
		bm25Index: bm25.NewIndex(),
	}

	prompt := orch.buildSystemPrompt()

	antiHallucinationPhrases := []string{
		"ONLY",
		"Do NOT use your general knowledge",
	}

	for _, phrase := range antiHallucinationPhrases {
		if !containsPhrase(prompt, phrase) {
			t.Errorf("default system prompt should contain '%s'", phrase)
		}
	}
}

func TestMinSimilarityConfigInSearchConfig(t *testing.T) {
	ms := 0.5
	cfg := config.SearchConfig{
		MinSimilarity: &ms,
	}

	if cfg.MinSimilarity == nil {
		t.Fatal("MinSimilarity should not be nil")
	}
	if *cfg.MinSimilarity != 0.5 {
		t.Errorf("expected MinSimilarity 0.5, got %v", *cfg.MinSimilarity)
	}
}

// TestBM25ToSearchResults_PreservesIDWithIDColumn verifies that when the
// table has a configured id_column, BM25 result ids are preserved so both
// search arms key on the same stable id during fusion.
func TestBM25ToSearchResults_PreservesIDWithIDColumn(t *testing.T) {
	bm25Results := []bm25.SearchResult{
		{ID: "42", Content: "doc-a"},
		{ID: "43", Content: "doc-b"},
	}

	out := bm25ToSearchResults(bm25Results, true)

	if len(out) != 2 {
		t.Fatalf("expected 2 results, got %d", len(out))
	}
	if out[0].ID != "42" || out[1].ID != "43" {
		t.Errorf("ids should be preserved with id_column set, got %q and %q",
			out[0].ID, out[1].ID)
	}
	if out[0].Content != "doc-a" || out[1].Content != "doc-b" {
		t.Errorf("content not carried through: %+v", out)
	}
}

// TestBM25ToSearchResults_ClearsIDWithoutIDColumn is a regression test for
// the no-id_column half of issue #27: without a stable id_column, the BM25
// arm's ROW_NUMBER() ids are not comparable to the vector arm, so they must
// be cleared. Otherwise BM25 keys by row number while the vector arm keys by
// content, leaving a document found by both arms duplicated instead of fused.
func TestBM25ToSearchResults_ClearsIDWithoutIDColumn(t *testing.T) {
	bm25Results := []bm25.SearchResult{
		{ID: "1", Content: "doc-a"},
		{ID: "2", Content: "doc-b"},
	}

	out := bm25ToSearchResults(bm25Results, false)

	if len(out) != 2 {
		t.Fatalf("expected 2 results, got %d", len(out))
	}
	for i, r := range out {
		if r.ID != "" {
			t.Errorf("result %d: id should be cleared without id_column, got %q", i, r.ID)
		}
	}
	// Content must still be present so fusion can key on it.
	if out[0].Content != "doc-a" || out[1].Content != "doc-b" {
		t.Errorf("content not carried through: %+v", out)
	}
}

// TestBM25ToSearchResults_FusesWithVectorArmWhenNoIDColumn ties the pieces
// together: with no id_column, vector results have empty ids (from
// buildVectorSearchQuery) and BM25 results have their ids cleared here, so a
// document returned by both arms fuses into ONE entry (keyed by content)
// rather than appearing twice.
func TestBM25ToSearchResults_FusesWithVectorArmWhenNoIDColumn(t *testing.T) {
	// Vector arm: no id_column -> empty ids, keyed by content.
	vectorResults := []database.SearchResult{
		{ID: "", Content: "shared-doc", Score: 0.9},
	}
	// BM25 arm returns the same document with a ROW_NUMBER id.
	bm25Raw := []bm25.SearchResult{
		{ID: "7", Content: "shared-doc", Score: 5.0},
	}

	bm25Results := bm25ToSearchResults(bm25Raw, false)

	fused := database.HybridSearch(vectorResults, bm25Results, 10, 0.5)

	if len(fused) != 1 {
		t.Fatalf("expected the shared document to fuse into 1 result, got %d: %+v",
			len(fused), fused)
	}
	if fused[0].Content != "shared-doc" {
		t.Errorf("expected fused content 'shared-doc', got %q", fused[0].Content)
	}
}

// TestBuildChatRequest_OmitsTemperature is a regression test: Temperature
// must stay nil so each provider/model uses its own default. A hardcoded
// value here previously broke requests to models that reject a
// temperature parameter outright (observed live against claude-sonnet-5:
// "400: `temperature` is deprecated for this model").
func TestBuildChatRequest_OmitsTemperature(t *testing.T) {
	orch := &Orchestrator{bm25Index: bm25.NewIndex()}

	req := orch.buildChatRequest(QueryRequest{Query: "hello"}, nil)

	if req.Temperature != nil {
		t.Errorf("expected Temperature to be nil (let the provider default apply), got %v", *req.Temperature)
	}
}

// TestRetrievalFailureError_AllTablesFailed is a regression test for
// issue #25: when every configured table's search failed and none
// produced results, retrievalFailureError must return a non-nil error so
// callers surface an infrastructure failure instead of a false "no
// relevant information" response.
func TestRetrievalFailureError_AllTablesFailed(t *testing.T) {
	err := retrievalFailureError(0, true, false)
	if err == nil {
		t.Fatal("expected a non-nil error when every table failed and none succeeded")
	}
}

// TestRetrievalFailureError_NoTablesConfigured verifies that having zero
// configured tables (hadError=false, hadSuccessfulLookup=false) is treated
// as a legitimate empty result, not a failure — there was nothing to fail.
func TestRetrievalFailureError_NoTablesConfigured(t *testing.T) {
	err := retrievalFailureError(0, false, false)
	if err != nil {
		t.Errorf("expected no error with no tables configured, got %v", err)
	}
}

// TestRetrievalFailureError_PartialFailureWithSuccessfulLookup verifies
// that a partial failure (some tables errored, but at least one search
// completed successfully) is NOT treated as a total failure, even if the
// successful table happened to return zero matching documents — that's a
// legitimate empty result, not an infrastructure problem.
func TestRetrievalFailureError_PartialFailureWithSuccessfulLookup(t *testing.T) {
	err := retrievalFailureError(0, true, true)
	if err != nil {
		t.Errorf("expected no error when at least one table's search succeeded, got %v", err)
	}
}

// TestRetrievalFailureError_ResultsPresent verifies that having any
// results at all short-circuits the failure check, regardless of the
// error/success flags — results in hand always mean a usable response.
func TestRetrievalFailureError_ResultsPresent(t *testing.T) {
	err := retrievalFailureError(1, true, false)
	if err != nil {
		t.Errorf("expected no error when results were found, got %v", err)
	}
}

// Verify mock providers implement the interfaces
var (
	_ Embedder  = (*MockEmbedder)(nil)
	_ Completer = (*MockCompleter)(nil)
)
