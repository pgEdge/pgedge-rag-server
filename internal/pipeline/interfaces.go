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
//
// Ping exposes the client's lightweight connectivity check for the
// /v1/health endpoint — see issue #23.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
	Ping(ctx context.Context) error
}

// Completer is the narrow interface the orchestrator needs from a
// chat-capable LLM client — non-streaming, streaming, and a
// connectivity check. The lib's llm.Client satisfies it structurally.
type Completer interface {
	Chat(ctx context.Context, req llmlib.ChatRequest) (*llmlib.ChatResponse, error)
	ChatStream(ctx context.Context, req llmlib.ChatRequest) (*llmlib.Stream, error)
	Ping(ctx context.Context) error
}
