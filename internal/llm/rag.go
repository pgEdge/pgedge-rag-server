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
	"fmt"
	"strings"
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
