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
	"log/slog"
	"net/textproto"
	"sync"
	"time"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/database"
	ragllm "github.com/pgEdge/pgedge-rag-server/internal/llm"
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
	embeddingProv  Embedder
	completionProv Completer
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

	// Create pipelines from configuration
	// Each pipeline loads its own API keys (cascaded from pipeline -> defaults -> global)
	ctx := context.Background()
	for _, pCfg := range cfg.Config.Pipelines {
		p, err := m.createPipeline(ctx, pCfg)
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
) (*Pipeline, error) {
	pipelineLogger := m.logger.With("pipeline", pCfg.Name)

	// Load API keys for this pipeline (uses pipeline-specific config, cascaded from defaults/global)
	keyLoader := config.NewAPIKeyLoader(pCfg.APIKeys)
	apiKeys, err := keyLoader.LoadKeysForPipeline(pCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to load API keys: %w", err)
	}

	// Create database connection pool
	dbPool, err := database.NewPool(ctx, pCfg.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create embedding client
	embeddingHeaders := mergeHeaders(pCfg.LLMHeaders, pCfg.EmbeddingLLM.Headers)
	embeddingProv, err := ragllm.NewEmbeddingClient(
		pCfg.EmbeddingLLM.Provider,
		pCfg.EmbeddingLLM.Model,
		pCfg.EmbeddingLLM.BaseURL,
		embeddingHeaders,
		apiKeys,
		ragllm.WithRequestTimeout(pCfg.EmbeddingLLM.RequestTimeout.Std()),
		ragllm.WithPerAttemptTimeout(pCfg.EmbeddingLLM.PerAttemptTimeout.Std()),
	)
	if err != nil {
		dbPool.Close()
		return nil, fmt.Errorf("failed to create embedding client: %w", err)
	}

	// Create completion client
	completionHeaders := mergeHeaders(pCfg.LLMHeaders, pCfg.RAGLLM.Headers)
	completionProv, err := ragllm.NewCompletionClient(
		pCfg.RAGLLM.Provider,
		pCfg.RAGLLM.Model,
		pCfg.RAGLLM.BaseURL,
		completionHeaders,
		apiKeys,
		ragllm.WithRequestTimeout(pCfg.RAGLLM.RequestTimeout.Std()),
		ragllm.WithPerAttemptTimeout(pCfg.RAGLLM.PerAttemptTimeout.Std()),
	)
	if err != nil {
		dbPool.Close()
		return nil, fmt.Errorf("failed to create completion client: %w", err)
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

// GetExecutor retrieves a pipeline by name as the narrower QueryExecutor
// interface, for callers (the HTTP server) that only need to run
// queries and shouldn't depend on *Pipeline directly — see issue #37.
//
// Deliberately does not just `return m.Get(name)`: on the not-found
// path Get returns a nil *Pipeline, and converting a nil *Pipeline
// straight into the QueryExecutor interface would produce a non-nil
// interface wrapping a nil pointer (a classic Go footgun), silently
// breaking any caller's `if executor == nil` check. Explicitly
// returning a literal nil on error avoids that.
func (m *Manager) GetExecutor(name string) (QueryExecutor, error) {
	p, err := m.Get(name)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Stats returns cumulative token usage for every pipeline.
func (m *Manager) Stats() []Usage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make([]Usage, 0, len(m.pipelines))
	for _, p := range m.pipelines {
		stats = append(stats, p.Usage())
	}

	return stats
}

// Health checks connectivity for every pipeline's providers
// concurrently, each bounded by DefaultPingTimeout, so the total call
// takes roughly one ping's worth of time regardless of how many
// pipelines are configured.
func (m *Manager) Health(ctx context.Context) []PipelineHealth {
	m.mu.RLock()
	pipelines := make([]*Pipeline, 0, len(m.pipelines))
	for _, p := range m.pipelines {
		pipelines = append(pipelines, p)
	}
	m.mu.RUnlock()

	results := make([]PipelineHealth, len(pipelines))
	var wg sync.WaitGroup
	for i, p := range pipelines {
		wg.Add(1)
		go func(i int, p *Pipeline) {
			defer wg.Done()
			results[i] = p.Ping(ctx)
		}(i, p)
	}
	wg.Wait()

	return results
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

// Usage returns this pipeline's cumulative embedding and completion
// token usage.
func (p *Pipeline) Usage() Usage {
	return Usage{
		Name:        p.name,
		Description: p.description,
		Embedding:   p.embeddingProv.Usage(),
		Completion:  p.completionProv.Usage(),
	}
}

// DefaultPingTimeout bounds how long a single provider's connectivity
// check is allowed to take before Ping reports it unreachable.
const DefaultPingTimeout = 3 * time.Second

// Ping checks connectivity for this pipeline's embedding and
// completion providers concurrently, each bounded by
// DefaultPingTimeout, so a slow or unreachable provider on one side
// doesn't add its timeout on top of the other's.
func (p *Pipeline) Ping(ctx context.Context) PipelineHealth {
	var embedding, completion ProviderHealth
	var wg sync.WaitGroup

	wg.Add(2)
	go func() {
		defer wg.Done()
		embedding = pingProvider(ctx, p.embeddingProv.Ping)
	}()
	go func() {
		defer wg.Done()
		completion = pingProvider(ctx, p.completionProv.Ping)
	}()
	wg.Wait()

	return PipelineHealth{
		Name:       p.name,
		Embedding:  embedding,
		Completion: completion,
	}
}

// pingProvider runs ping with a DefaultPingTimeout deadline and
// converts the result into a ProviderHealth. A panic from ping (e.g. a
// buggy provider client) is recovered and reported as unreachable
// rather than crashing the whole process: this runs inside goroutines
// spawned by Pipeline.Ping/Manager.Health, and Go's panic/recover is
// per-goroutine, so recoveryMiddleware's recover on the request
// goroutine can't catch it.
func pingProvider(ctx context.Context, ping func(context.Context) error) (health ProviderHealth) {
	defer func() {
		if r := recover(); r != nil {
			health = ProviderHealth{Reachable: false, Error: fmt.Sprintf("panic: %v", r)}
		}
	}()

	pingCtx, cancel := context.WithTimeout(ctx, DefaultPingTimeout)
	defer cancel()

	if err := ping(pingCtx); err != nil {
		return ProviderHealth{Reachable: false, Error: err.Error()}
	}

	return ProviderHealth{Reachable: true}
}

// Close releases resources associated with the pipeline.
func (p *Pipeline) Close() {
	if p.dbPool != nil {
		p.dbPool.Close()
	}
}

// mergeHeaders merges pipeline-level and per-LLM headers.
// Per-LLM headers take precedence over pipeline-level headers.
// Keys are canonicalized so that "x-api-key" and "X-Api-Key"
// resolve to the same header.
func mergeHeaders(
	pipelineHeaders, llmHeaders map[string]string,
) map[string]string {
	if len(pipelineHeaders) == 0 && len(llmHeaders) == 0 {
		return nil
	}
	merged := make(map[string]string)
	for k, v := range pipelineHeaders {
		merged[textproto.CanonicalMIMEHeaderKey(k)] = v
	}
	for k, v := range llmHeaders {
		merged[textproto.CanonicalMIMEHeaderKey(k)] = v
	}
	return merged
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
