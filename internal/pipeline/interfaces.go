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
)

// Embedder is the narrow interface the orchestrator needs from an
// embedding-capable LLM client. The lib's llm.Client satisfies it
// structurally; orchestrator tests provide a one-method mock.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
}

// Completer is the narrow interface the orchestrator needs from a
// chat-capable LLM client. Two methods only — non-streaming and
// streaming. The lib's llm.Client satisfies it structurally.
type Completer interface {
	Chat(ctx context.Context, req llmlib.ChatRequest) (*llmlib.ChatResponse, error)
	ChatStream(ctx context.Context, req llmlib.ChatRequest) (*llmlib.Stream, error)
}

// Reranker is the narrow interface the orchestrator needs from a
// rerank-capable LLM client. The lib's llm.Client satisfies it
// structurally; orchestrator tests provide a one-method mock.
type Reranker interface {
	Rerank(ctx context.Context, req llmlib.RerankRequest) (*llmlib.RerankResponse, error)
}
