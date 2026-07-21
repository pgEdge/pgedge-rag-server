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

	llmlib "github.com/pgEdge/pgedge-go-llm-lib/llm"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/database"
)

// Embedder is the narrow interface the orchestrator needs from an
// embedding-capable LLM client. The lib's llm.Client satisfies it
// structurally; orchestrator tests provide a one-method mock.
//
// Usage exposes the client's cumulative token usage (since creation or
// its last ResetUsage call) for the /v1/stats endpoint — see issue #21.
// Ping exposes the client's lightweight connectivity check for the
// /v1/health endpoint — see issue #23.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Usage() llmlib.TokenUsage
	Ping(ctx context.Context) error
}

// Completer is the narrow interface the orchestrator needs from a
// chat-capable LLM client — non-streaming, streaming, cumulative
// usage, and a connectivity check. The lib's llm.Client satisfies it
// structurally.
type Completer interface {
	Chat(ctx context.Context, req llmlib.ChatRequest) (*llmlib.ChatResponse, error)
	ChatStream(ctx context.Context, req llmlib.ChatRequest) (*llmlib.Stream, error)
	Usage() llmlib.TokenUsage
	Ping(ctx context.Context) error
}

// SearchBackend is the narrow interface the orchestrator's search()
// needs from the database layer. The concrete *database.Pool satisfies
// it structurally. Narrowing this lets tests drive search() to fail (or
// partially fail) on demand, without a real database — see issue #37.
type SearchBackend interface {
	VectorSearch(
		ctx context.Context,
		embedding []float32,
		table config.TableSource,
		topN int,
		filter *config.Filter,
		minSimilarity *float64,
	) ([]database.SearchResult, error)

	FetchDocuments(
		ctx context.Context,
		table config.TableSource,
		filter *config.Filter,
	) (map[string]string, error)
}

// QueryExecutor is the narrow interface the server needs from a
// pipeline to run a query. *Pipeline satisfies it structurally. Server
// tests provide a fake that can hang (respecting context cancellation),
// error, or return a controlled result, without a real pipeline — see
// issue #37.
type QueryExecutor interface {
	ExecuteWithOptions(ctx context.Context, req QueryRequest) (*QueryResponse, error)
	ExecuteStreamWithOptions(ctx context.Context, req QueryRequest) (<-chan StreamChunk, <-chan error)
}

// Reranker is the narrow interface the orchestrator needs from a
// rerank-capable LLM client. The lib's llm.Client satisfies it
// structurally; orchestrator tests provide a one-method mock.
type Reranker interface {
	Rerank(ctx context.Context, req llmlib.RerankRequest) (*llmlib.RerankResponse, error)
}
