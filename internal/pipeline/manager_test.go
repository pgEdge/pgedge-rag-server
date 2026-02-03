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
	"log/slog"
	"testing"

	"github.com/pgEdge/pgedge-rag-server/internal/bm25"
	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/llm"
)

// newTestManager creates a Manager with mock pipelines for testing.
// This bypasses database and LLM provider initialization.
func newTestManager(cfg *config.Config) *Manager {
	m := &Manager{
		pipelines: make(map[string]*Pipeline),
		config:    cfg,
	}

	for _, pCfg := range cfg.Pipelines {
		// Create mock providers
		embeddingProv := &MockEmbeddingProvider{}
		completionProv := &MockCompletionProvider{}

		// Create a copy of pCfg for the pointer
		pCfgCopy := pCfg

		// Create orchestrator with mock providers
		orchestrator := NewOrchestrator(OrchestratorConfig{
			Pipeline:       &pCfgCopy,
			EmbeddingProv:  embeddingProv,
			CompletionProv: completionProv,
			TokenBudget:    DefaultTokenBudget,
			TopN:           DefaultTopN,
		})

		m.pipelines[pCfg.Name] = &Pipeline{
			name:           pCfg.Name,
			description:    pCfg.Description,
			config:         pCfg,
			embeddingProv:  embeddingProv,
			completionProv: completionProv,
			orchestrator:   orchestrator,
		}
	}

	return m
}

// newTestPipeline creates a Pipeline with mock providers for testing.
func newTestPipeline(name, description string) *Pipeline {
	embeddingProv := &MockEmbeddingProvider{}
	completionProv := &MockCompletionProvider{
		CompleteFunc: func(ctx context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content:      "Test response for: " + req.Messages[0].Content,
				FinishReason: "stop",
				Usage: llm.TokenUsage{
					PromptTokens:     100,
					CompletionTokens: 20,
					TotalTokens:      120,
				},
			}, nil
		},
	}

	pCfg := config.Pipeline{
		Name:        name,
		Description: description,
		Tables:      []config.TableSource{},
	}

	orchestrator := &Orchestrator{
		cfg:            &pCfg,
		embeddingProv:  embeddingProv,
		completionProv: completionProv,
		bm25Index:      bm25.NewIndex(),
		tokenBudget:    DefaultTokenBudget,
		topN:           DefaultTopN,
		logger:         slog.Default(),
	}

	return &Pipeline{
		name:           name,
		description:    description,
		config:         pCfg,
		embeddingProv:  embeddingProv,
		completionProv: completionProv,
		orchestrator:   orchestrator,
	}
}

func testConfig() *config.Config {
	return &config.Config{
		Pipelines: []config.Pipeline{
			{
				Name:        "pipeline-1",
				Description: "First test pipeline",
				Database: config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Database: "testdb",
				},
				Tables: []config.TableSource{
					{
						Table:        "documents",
						TextColumn:   "content",
						VectorColumn: "embedding",
					},
				},
				EmbeddingLLM: config.LLMConfig{
					Provider: "openai",
					Model:    "text-embedding-3-small",
				},
				RAGLLM: config.LLMConfig{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-20250514",
				},
				TokenBudget: 1000,
				TopN:        10,
			},
			{
				Name:        "pipeline-2",
				Description: "Second test pipeline",
				Database: config.DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Database: "testdb2",
				},
				Tables: []config.TableSource{
					{
						Table:        "docs",
						TextColumn:   "text",
						VectorColumn: "vec",
					},
				},
				EmbeddingLLM: config.LLMConfig{
					Provider: "voyage",
					Model:    "voyage-2",
				},
				RAGLLM: config.LLMConfig{
					Provider: "openai",
					Model:    "gpt-4",
				},
				TokenBudget: 2000,
				TopN:        20,
			},
		},
	}
}

func TestNewManager(t *testing.T) {
	cfg := testConfig()
	m := newTestManager(cfg)
	defer func() { _ = m.Close() }()

	if m == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestManager_List(t *testing.T) {
	cfg := testConfig()
	m := newTestManager(cfg)
	defer func() { _ = m.Close() }()

	infos := m.List()
	if len(infos) != 2 {
		t.Errorf("expected 2 pipelines, got %d", len(infos))
	}

	// Check that both pipelines are in the list
	names := make(map[string]bool)
	for _, info := range infos {
		names[info.Name] = true
	}

	if !names["pipeline-1"] {
		t.Error("expected pipeline-1 in list")
	}
	if !names["pipeline-2"] {
		t.Error("expected pipeline-2 in list")
	}
}

func TestManager_Get(t *testing.T) {
	cfg := testConfig()
	m := newTestManager(cfg)
	defer func() { _ = m.Close() }()

	p, err := m.Get("pipeline-1")
	if err != nil {
		t.Fatalf("failed to get pipeline: %v", err)
	}

	if p.Name() != "pipeline-1" {
		t.Errorf("expected name 'pipeline-1', got '%s'", p.Name())
	}

	if p.Description() != "First test pipeline" {
		t.Errorf("expected description 'First test pipeline', got '%s'",
			p.Description())
	}
}

func TestManager_Get_NotFound(t *testing.T) {
	cfg := testConfig()
	m := newTestManager(cfg)
	defer func() { _ = m.Close() }()

	_, err := m.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent pipeline")
	}

	if !errors.Is(err, ErrPipelineNotFound) {
		t.Errorf("expected ErrPipelineNotFound, got %v", err)
	}
}

func TestPipeline_Execute_NoDocuments(t *testing.T) {
	// Create a test pipeline with no documents configured
	p := newTestPipeline("test-pipeline", "Test pipeline")

	// Execute should return an error when no documents are found
	_, err := p.ExecuteWithOptions(context.Background(), QueryRequest{
		Query: "test query",
	})
	if err == nil {
		t.Fatal("expected error when no documents found")
	}

	expectedErr := "no documents found for query"
	if err.Error() != expectedErr {
		t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestPipeline_ExecuteStream_NoDocuments(t *testing.T) {
	// Create a test pipeline with no documents configured
	embeddingProv := &MockEmbeddingProvider{}
	completionProv := &MockCompletionProvider{}

	pCfg := config.Pipeline{
		Name:        "stream-test",
		Description: "Streaming test pipeline",
		Tables:      []config.TableSource{},
	}

	orchestrator := &Orchestrator{
		cfg:            &pCfg,
		embeddingProv:  embeddingProv,
		completionProv: completionProv,
		bm25Index:      bm25.NewIndex(),
		tokenBudget:    DefaultTokenBudget,
		topN:           DefaultTopN,
		logger:         slog.Default(),
	}

	p := &Pipeline{
		name:           "stream-test",
		description:    "Streaming test pipeline",
		config:         pCfg,
		embeddingProv:  embeddingProv,
		completionProv: completionProv,
		orchestrator:   orchestrator,
	}

	// Execute streaming query - should return error when no documents
	chunkChan, errChan := p.ExecuteStreamWithOptions(context.Background(), QueryRequest{
		Query:  "test query",
		Stream: true,
	})

	// Drain the chunk channel
	for range chunkChan {
	}

	err := <-errChan
	if err == nil {
		t.Fatal("expected error when no documents found")
	}

	expectedErr := "no documents found for query"
	if err.Error() != expectedErr {
		t.Errorf("expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestManager_Close(t *testing.T) {
	cfg := testConfig()
	m := newTestManager(cfg)

	err := m.Close()
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Verify pipelines are nil after close
	if m.pipelines != nil {
		t.Error("expected pipelines to be nil after close")
	}
}
