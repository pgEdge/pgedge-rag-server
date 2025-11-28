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
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/database"
	"github.com/pgEdge/pgedge-rag-server/internal/llm"
	"github.com/pgEdge/pgedge-rag-server/internal/llm/factory"
)

// ErrPipelineNotFound is returned when a requested pipeline does not exist.
var ErrPipelineNotFound = errors.New("pipeline not found")

// Default values for pipeline configuration
const (
	DefaultTokenBudget = 4000
	DefaultTopN        = 5
)

// Manager manages the lifecycle of RAG pipelines.
type Manager struct {
	mu        sync.RWMutex
	pipelines map[string]*Pipeline
	config    *config.Config
	logger    *slog.Logger
}

// Pipeline represents a configured RAG pipeline with all providers initialized.
type Pipeline struct {
	name           string
	description    string
	config         config.Pipeline
	dbPool         *database.Pool
	embeddingProv  llm.EmbeddingProvider
	completionProv llm.CompletionProvider
	orchestrator   *Orchestrator
	logger         *slog.Logger
}

// ManagerConfig contains configuration for creating a Manager.
type ManagerConfig struct {
	Config *config.Config
	Logger *slog.Logger
}

// NewManager creates a new pipeline manager from configuration.
func NewManager(cfg *config.Config) (*Manager, error) {
	return NewManagerWithLogger(ManagerConfig{
		Config: cfg,
		Logger: slog.Default(),
	})
}

// NewManagerWithLogger creates a new pipeline manager with a custom logger.
func NewManagerWithLogger(cfg ManagerConfig) (*Manager, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	m := &Manager{
		pipelines: make(map[string]*Pipeline),
		config:    cfg.Config,
		logger:    logger,
	}

	// Load API keys from config file paths, environment variables, or defaults
	keyLoader := config.NewAPIKeyLoader(cfg.Config.APIKeys)
	apiKeys, err := keyLoader.LoadRequiredKeys(cfg.Config.Pipelines)
	if err != nil {
		return nil, fmt.Errorf("failed to load API keys: %w", err)
	}

	// Create pipelines from configuration
	ctx := context.Background()
	for _, pCfg := range cfg.Config.Pipelines {
		p, err := m.createPipeline(ctx, pCfg, apiKeys)
		if err != nil {
			// Clean up any already created pipelines
			for _, existing := range m.pipelines {
				existing.Close()
			}
			return nil, fmt.Errorf("failed to create pipeline %s: %w", pCfg.Name, err)
		}
		m.pipelines[pCfg.Name] = p
		logger.Info("pipeline created",
			"name", pCfg.Name,
			"embedding_provider", pCfg.EmbeddingLLM.Provider,
			"completion_provider", pCfg.RAGLLM.Provider,
		)
	}

	return m, nil
}

// createPipeline creates a single pipeline with all providers initialized.
func (m *Manager) createPipeline(
	ctx context.Context,
	pCfg config.Pipeline,
	apiKeys *config.LoadedKeys,
) (*Pipeline, error) {
	pipelineLogger := m.logger.With("pipeline", pCfg.Name)

	// Create database connection pool
	dbPool, err := database.NewPool(ctx, pCfg.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create embedding provider
	embeddingProv, err := factory.NewEmbeddingProvider(
		pCfg.EmbeddingLLM.Provider,
		pCfg.EmbeddingLLM.Model,
		apiKeys,
	)
	if err != nil {
		dbPool.Close()
		return nil, fmt.Errorf("failed to create embedding provider: %w", err)
	}

	// Create completion provider
	completionProv, err := factory.NewCompletionProvider(
		pCfg.RAGLLM.Provider,
		pCfg.RAGLLM.Model,
		apiKeys,
	)
	if err != nil {
		dbPool.Close()
		return nil, fmt.Errorf("failed to create completion provider: %w", err)
	}

	// Determine token budget: pipeline > global defaults > hardcoded default
	tokenBudget := DefaultTokenBudget
	if m.config.Defaults.TokenBudget > 0 {
		tokenBudget = m.config.Defaults.TokenBudget
	}
	if pCfg.TokenBudget > 0 {
		tokenBudget = pCfg.TokenBudget
	}

	// Determine topN: pipeline > global defaults > hardcoded default
	topN := DefaultTopN
	if m.config.Defaults.TopN > 0 {
		topN = m.config.Defaults.TopN
	}
	if pCfg.TopN > 0 {
		topN = pCfg.TopN
	}

	// Create orchestrator
	orchestrator := NewOrchestrator(OrchestratorConfig{
		Pipeline:       &pCfg,
		DBPool:         dbPool,
		EmbeddingProv:  embeddingProv,
		CompletionProv: completionProv,
		TokenBudget:    tokenBudget,
		TopN:           topN,
		Logger:         pipelineLogger,
	})

	return &Pipeline{
		name:           pCfg.Name,
		description:    pCfg.Description,
		config:         pCfg,
		dbPool:         dbPool,
		embeddingProv:  embeddingProv,
		completionProv: completionProv,
		orchestrator:   orchestrator,
		logger:         pipelineLogger,
	}, nil
}

// List returns information about all available pipelines.
func (m *Manager) List() []Info {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]Info, 0, len(m.pipelines))
	for _, p := range m.pipelines {
		infos = append(infos, Info{
			Name:        p.name,
			Description: p.description,
		})
	}

	return infos
}

// Get retrieves a pipeline by name.
func (m *Manager) Get(name string) (*Pipeline, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.pipelines[name]
	if !ok {
		return nil, ErrPipelineNotFound
	}

	return p, nil
}

// Execute runs a RAG query on the pipeline.
func (p *Pipeline) Execute(ctx context.Context, query string) (*QueryResponse, error) {
	return p.orchestrator.Execute(ctx, QueryRequest{
		Query:  query,
		Stream: false,
	})
}

// ExecuteWithOptions runs a RAG query with custom options.
func (p *Pipeline) ExecuteWithOptions(
	ctx context.Context,
	req QueryRequest,
) (*QueryResponse, error) {
	return p.orchestrator.Execute(ctx, req)
}

// ExecuteStream runs a RAG query and returns a streaming response.
func (p *Pipeline) ExecuteStream(
	ctx context.Context,
	query string,
) (<-chan StreamChunk, <-chan error) {
	return p.orchestrator.ExecuteStream(ctx, QueryRequest{
		Query:  query,
		Stream: true,
	})
}

// ExecuteStreamWithOptions runs a streaming RAG query with custom options.
func (p *Pipeline) ExecuteStreamWithOptions(
	ctx context.Context,
	req QueryRequest,
) (<-chan StreamChunk, <-chan error) {
	req.Stream = true
	return p.orchestrator.ExecuteStream(ctx, req)
}

// Name returns the pipeline name.
func (p *Pipeline) Name() string {
	return p.name
}

// Description returns the pipeline description.
func (p *Pipeline) Description() string {
	return p.description
}

// Close releases resources associated with the pipeline.
func (p *Pipeline) Close() {
	if p.dbPool != nil {
		p.dbPool.Close()
	}
}

// Close shuts down the manager and releases resources.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, p := range m.pipelines {
		p.Close()
	}
	m.pipelines = nil

	return nil
}
