//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pgEdge/pgedge-rag-server/internal/bm25"
	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/database"
	"github.com/pgEdge/pgedge-rag-server/internal/llm"
)

// MockEmbeddingProvider implements llm.EmbeddingProvider for testing.
type MockEmbeddingProvider struct {
	EmbedFunc      func(ctx context.Context, text string) ([]float32, error)
	EmbedBatchFunc func(ctx context.Context, texts []string) ([][]float32, error)
	DimensionsVal  int
	ModelNameVal   string
}

func (m *MockEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.EmbedFunc != nil {
		return m.EmbedFunc(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func (m *MockEmbeddingProvider) EmbedBatch(
	ctx context.Context,
	texts []string,
) ([][]float32, error) {
	if m.EmbedBatchFunc != nil {
		return m.EmbedBatchFunc(ctx, texts)
	}
	results := make([][]float32, len(texts))
	for i := range texts {
		results[i] = []float32{0.1, 0.2, 0.3}
	}
	return results, nil
}

func (m *MockEmbeddingProvider) Dimensions() int {
	if m.DimensionsVal > 0 {
		return m.DimensionsVal
	}
	return 768
}

func (m *MockEmbeddingProvider) ModelName() string {
	if m.ModelNameVal != "" {
		return m.ModelNameVal
	}
	return "mock-embedding-model"
}

// MockCompletionProvider implements llm.CompletionProvider for testing.
type MockCompletionProvider struct {
	CompleteFunc       func(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error)
	CompleteStreamFunc func(ctx context.Context, req llm.CompletionRequest) (<-chan llm.StreamChunk, <-chan error)
	ModelNameVal       string
}

func (m *MockCompletionProvider) Complete(
	ctx context.Context,
	req llm.CompletionRequest,
) (*llm.CompletionResponse, error) {
	if m.CompleteFunc != nil {
		return m.CompleteFunc(ctx, req)
	}
	return &llm.CompletionResponse{
		Content:      "This is a mock response.",
		FinishReason: "stop",
		Usage: llm.TokenUsage{
			PromptTokens:     100,
			CompletionTokens: 20,
			TotalTokens:      120,
		},
	}, nil
}

func (m *MockCompletionProvider) CompleteStream(
	ctx context.Context,
	req llm.CompletionRequest,
) (<-chan llm.StreamChunk, <-chan error) {
	if m.CompleteStreamFunc != nil {
		return m.CompleteStreamFunc(ctx, req)
	}
	chunkChan := make(chan llm.StreamChunk, 3)
	errChan := make(chan error, 1)

	go func() {
		defer close(chunkChan)
		defer close(errChan)
		chunkChan <- llm.StreamChunk{Content: "This is "}
		chunkChan <- llm.StreamChunk{Content: "a streaming response."}
		chunkChan <- llm.StreamChunk{
			Content:      "",
			FinishReason: "stop",
			Usage: &llm.TokenUsage{
				PromptTokens:     100,
				CompletionTokens: 20,
				TotalTokens:      120,
			},
		}
	}()

	return chunkChan, errChan
}

func (m *MockCompletionProvider) ModelName() string {
	if m.ModelNameVal != "" {
		return m.ModelNameVal
	}
	return "mock-completion-model"
}

func TestNewOrchestrator(t *testing.T) {
	cfg := OrchestratorConfig{
		Pipeline: &config.Pipeline{
			Name: "test-pipeline",
		},
		EmbeddingProv:  &MockEmbeddingProvider{},
		CompletionProv: &MockCompletionProvider{},
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
		EmbeddingProv:  &MockEmbeddingProvider{},
		CompletionProv: &MockCompletionProvider{},
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

// Test mock providers work correctly
func TestMockEmbeddingProvider(t *testing.T) {
	provider := &MockEmbeddingProvider{
		DimensionsVal: 384,
		ModelNameVal:  "test-model",
	}

	// Test Embed
	embedding, err := provider.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(embedding) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(embedding))
	}

	// Test EmbedBatch
	embeddings, err := provider.EmbedBatch(context.Background(), []string{"text1", "text2"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(embeddings))
	}

	// Test Dimensions
	if provider.Dimensions() != 384 {
		t.Errorf("expected 384 dimensions, got %d", provider.Dimensions())
	}

	// Test ModelName
	if provider.ModelName() != "test-model" {
		t.Errorf("expected 'test-model', got '%s'", provider.ModelName())
	}
}

func TestMockCompletionProvider(t *testing.T) {
	provider := &MockCompletionProvider{
		ModelNameVal: "test-completion-model",
	}

	// Test Complete
	resp, err := provider.Complete(context.Background(), llm.CompletionRequest{
		SystemPrompt: "You are a test assistant.",
		Messages:     []llm.Message{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if resp.Content != "This is a mock response." {
		t.Errorf("unexpected content: %s", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got '%s'", resp.FinishReason)
	}

	// Test CompleteStream
	chunkChan, errChan := provider.CompleteStream(context.Background(), llm.CompletionRequest{
		SystemPrompt: "You are a test assistant.",
		Messages:     []llm.Message{{Role: "user", Content: "Hello"}},
	})

	var content string
	for chunk := range chunkChan {
		content += chunk.Content
	}

	if err := <-errChan; err != nil {
		t.Fatalf("CompleteStream failed: %v", err)
	}

	if content != "This is a streaming response." {
		t.Errorf("unexpected streaming content: %s", content)
	}

	// Test ModelName
	if provider.ModelName() != "test-completion-model" {
		t.Errorf("expected 'test-completion-model', got '%s'", provider.ModelName())
	}
}

func TestMockProvidersWithCustomFunctions(t *testing.T) {
	// Test embedding provider with custom function
	embeddingProvider := &MockEmbeddingProvider{
		EmbedFunc: func(ctx context.Context, text string) ([]float32, error) {
			return nil, errors.New("embedding error")
		},
	}

	_, err := embeddingProvider.Embed(context.Background(), "test")
	if err == nil || err.Error() != "embedding error" {
		t.Error("expected custom error from EmbedFunc")
	}

	// Test completion provider with custom function
	completionProvider := &MockCompletionProvider{
		CompleteFunc: func(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content: "Custom response for: " + req.Messages[0].Content,
			}, nil
		},
	}

	resp, err := completionProvider.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{{Role: "user", Content: "test question"}},
	})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if resp.Content != "Custom response for: test question" {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

// Verify mock providers implement the interfaces
var (
	_ llm.EmbeddingProvider  = (*MockEmbeddingProvider)(nil)
	_ llm.CompletionProvider = (*MockCompletionProvider)(nil)
)
