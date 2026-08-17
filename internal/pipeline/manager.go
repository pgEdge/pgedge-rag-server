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
	"github.com/pgEdge/pgedge-rag-server/internal/safeerr"
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
	dbPool, err := database.NewPool(ctx, pCfg.Database, m.config.Identity)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Confirm the database will act on the identity this server
	// presents, before serving a single request with it. See
	// database.VerifyEnforcement for why this is checked at startup
	// rather than left to be noticed later: every case it detects is one
	// where retrieval works and returns rows whilst row-level security
	// is not evaluating the caller.
	if err := verifyEnforcement(ctx, pipelineLogger, m.config.Identity,
		dbPool, pCfg.Tables); err != nil {
		dbPool.Close()
		return nil, err
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

	// Create rerank client (optional; disabled unless a provider is
	// configured for this pipeline's rerank stage).
	var reranker Reranker
	if pCfg.Rerank.Provider != "" {
		rerankHeaders := mergeHeaders(pCfg.LLMHeaders, pCfg.Rerank.Headers)
		reranker, err = ragllm.NewRerankClient(
			pCfg.Rerank.Provider,
			pCfg.Rerank.Model,
			pCfg.Rerank.BaseURL,
			rerankHeaders,
			apiKeys,
			ragllm.WithRequestTimeout(pCfg.Rerank.RequestTimeout.Std()),
			ragllm.WithPerAttemptTimeout(pCfg.Rerank.PerAttemptTimeout.Std()),
		)
		if err != nil {
			dbPool.Close()
			return nil, fmt.Errorf("failed to create rerank client: %w", err)
		}
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
		Reranker:       reranker,
		RerankTopK:     pCfg.Rerank.TopK,
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

// verifyEnforcement runs the startup identity-enforcement preflight and
// applies the configured enforcement_check policy to its findings.
//
// Under "error" (the default) any finding aborts startup. Under "warn"
// every finding is logged at warning level and startup continues, which
// is the setting for a deployment whose policies read the claims
// indirectly and so cannot be verified textually. Under "off" the
// preflight does not run at all.
//
// The warn path logs each finding separately rather than one summary
// line, because each names a different table and a different remedy.
func verifyEnforcement(
	ctx context.Context,
	logger *slog.Logger,
	idCfg config.IdentityConfig,
	pool enforcementVerifier,
	tables []config.TableSource,
) error {
	problems := pool.VerifyEnforcement(ctx, tables)
	if len(problems) == 0 {
		return nil
	}

	if idCfg.WithDefaults().EnforcementCheck == config.EnforcementCheckWarn {
		for _, problem := range problems {
			logger.Warn("identity enforcement could not be verified; "+
				"serving anyway because identity.enforcement_check is set to warn",
				"problem", problem.Error())
		}
		return nil
	}

	return errors.Join(problems...)
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
//
// Ping goes through the same client the pipeline uses for real
// requests, so it inherits that client's retry policy: the
// pgedge-go-llm-lib default is up to 5 retries with a 2-second initial
// backoff. A perfectly healthy provider that just happens to need one
// ordinary retry can therefore burn 2+ seconds on backoff alone before
// its second attempt even starts. A 3-second budget left no room for
// that, so a single routine retry was enough to report a healthy
// provider as "unreachable" (issue #55). 10 seconds comfortably covers
// one retry cycle whilst still keeping /v1/health responsive.
const DefaultPingTimeout = 10 * time.Second

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
		embedding = pingProvider(ctx, p.logger, p.name, "embedding", p.embeddingProv.Ping)
	}()
	go func() {
		defer wg.Done()
		completion = pingProvider(ctx, p.logger, p.name, "completion", p.completionProv.Ping)
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
//
// The reported Error is a fixed, classified description rather than the
// underlying error's text. A failing ping wraps the provider's own error
// body, and providers echo a truncated form of the submitted API key on
// an authentication failure, so putting that text here would publish
// part of a real credential: GET /v1/health is unauthenticated and
// serialises this field straight to the caller. Full detail goes to the
// log instead, with credential-shaped strings scrubbed.
func pingProvider(
	ctx context.Context,
	logger *slog.Logger,
	pipelineName string,
	kind string,
	ping func(context.Context) error,
) (health ProviderHealth) {
	if logger == nil {
		logger = slog.Default()
	}

	defer func() {
		if r := recover(); r != nil {
			// A panic value can be anything, including something that
			// embeds a request or credential, so it is logged rather
			// than reported.
			logger.Error("provider ping panicked",
				"pipeline", pipelineName,
				"provider_kind", kind,
				"panic", safeerr.Redact(fmt.Sprintf("%v", r)))
			health = ProviderHealth{Reachable: false, Error: safeerr.MsgInternal}
		}
	}()

	pingCtx, cancel := context.WithTimeout(ctx, DefaultPingTimeout)
	defer cancel()

	if err := ping(pingCtx); err != nil {
		logger.Warn("provider ping failed",
			"pipeline", pipelineName,
			"provider_kind", kind,
			"error", safeerr.RedactError(err))
		return ProviderHealth{Reachable: false, Error: safeerr.Message(err)}
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
