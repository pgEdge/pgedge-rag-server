//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package llm

import (
	"strings"
	"testing"
)

func TestFormatContext_EmptyDocs(t *testing.T) {
	got := FormatContext(nil)
	want := "Use the following context to answer the question:\n\n"
	if got != want {
		t.Errorf("FormatContext(nil) = %q, want %q", got, want)
	}
}

func TestFormatContext_WithSource(t *testing.T) {
	docs := []ContextDoc{
		{Content: "First doc body", Source: "doc-a", Score: 0.9},
	}
	got := FormatContext(docs)

	wantContains := []string{
		"Use the following context to answer the question:",
		"--- Document 1 (Source: doc-a) ---",
		"First doc body",
	}
	for _, s := range wantContains {
		if !strings.Contains(got, s) {
			t.Errorf("FormatContext output missing %q\n--- got ---\n%s", s, got)
		}
	}
}

func TestFormatContext_NoSource(t *testing.T) {
	docs := []ContextDoc{
		{Content: "Body without source"},
	}
	got := FormatContext(docs)

	if !strings.Contains(got, "--- Document 1 ---") {
		t.Errorf("expected '--- Document 1 ---' header (no source suffix)\n--- got ---\n%s", got)
	}
	if strings.Contains(got, "Source:") {
		t.Errorf("expected no 'Source:' suffix when Source is empty\n--- got ---\n%s", got)
	}
}

func TestFormatContext_OrderingAndNumbering(t *testing.T) {
	docs := []ContextDoc{
		{Content: "alpha", Source: "a"},
		{Content: "beta", Source: "b"},
		{Content: "gamma", Source: "c"},
	}
	got := FormatContext(docs)

	for i, source := range []string{"a", "b", "c"} {
		header := "--- Document " + string(rune('1'+i)) + " (Source: " + source + ") ---"
		if !strings.Contains(got, header) {
			t.Errorf("expected header %q in output\n--- got ---\n%s", header, got)
		}
	}
}
