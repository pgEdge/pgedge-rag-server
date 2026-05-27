//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package llm exposes RAG-server-specific helpers around the shared
// pgedge-go-llm-lib. Provider construction lives in factory.go;
// per-request helpers live here.
package llm

import (
	"context"
	"fmt"
	"strings"

	llmlib "github.com/pgEdge/pgedge-go-llm-lib/llm"
)

// ContextDoc is a single retrieved document passed to an LLM as
// grounding context. The orchestrator builds a slice of these from
// search results before formatting them into the system prompt.
type ContextDoc struct {
	Content string
	Source  string
	Score   float64
}

// FormatContext renders retrieved documents as a block of text to
// prepend to (or include alongside) the system prompt. Output format
// is stable across releases — pipeline tests rely on the header
// strings.
func FormatContext(docs []ContextDoc) string {
	var sb strings.Builder
	sb.WriteString("Use the following context to answer the question:\n\n")

	for i, doc := range docs {
		fmt.Fprintf(&sb, "--- Document %d", i+1)
		if doc.Source != "" {
			fmt.Fprintf(&sb, " (Source: %s)", doc.Source)
		}
		sb.WriteString(" ---\n")
		sb.WriteString(doc.Content)
		sb.WriteString("\n\n")
	}

	return sb.String()
}

// embedder is the minimal interface Embed32 needs from a client.
// The lib's llm.Client satisfies it structurally — there is no
// runtime conversion or wrapper. Defined locally so tests can stub
// without depending on the lib.
type embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
}

// Embed32 returns the embedding for text as a []float32 — pgvector
// expects float32, and this is the only place we narrow.
func Embed32(ctx context.Context, c embedder, text string) ([]float32, error) {
	vec, err := c.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	out := make([]float32, len(vec))
	for i, v := range vec {
		out[i] = float32(v)
	}
	return out, nil
}

// StopReasonString maps the lib's normalised stop reason to the
// finish_reason string the RAG server emits in streaming and
// non-streaming responses. Preserved verbatim from the pre-migration
// behaviour to avoid breaking API consumers that inspect the field.
//
// Unknown values fall back to "stop" — the most common case for
// "model finished cleanly".
func StopReasonString(r llmlib.StopReason) string {
	switch r {
	case llmlib.StopReasonEndTurn:
		return "stop"
	case llmlib.StopReasonMaxTokens:
		return "length"
	case llmlib.StopReasonStopSequence:
		return "stop_sequence"
	case llmlib.StopReasonToolUse:
		return "tool_use"
	case llmlib.StopReasonContentFilter:
		return "content_filter"
	case llmlib.StopReasonError:
		return "error"
	default:
		return "stop"
	}
}
