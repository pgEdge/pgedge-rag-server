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
	"context"
	"errors"
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

// stubEmbedClient implements just the Embed method of llm.Client for
// testing Embed32. All other methods are unused; we don't need a full
// llm.Client because Embed32 doesn't take one — see note in body.
//
// Note: Embed32 takes an interface{ Embed(ctx, text) ([]float64, error) }
// to keep tests cheap. The real argument type is llm.Client from the
// shared lib, which satisfies the local interface structurally.

type stubEmbedClient struct {
	vec []float64
	err error
}

func (s *stubEmbedClient) Embed(ctx context.Context, text string) ([]float64, error) {
	return s.vec, s.err
}

func TestEmbed32_ConvertsFloat64ToFloat32(t *testing.T) {
	stub := &stubEmbedClient{vec: []float64{0.1, -0.25, 1.5, 0}}
	got, err := Embed32(context.Background(), stub, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []float32{0.1, -0.25, 1.5, 0}
	if len(got) != len(want) {
		t.Fatalf("len(got)=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%v, want %v", i, got[i], want[i])
		}
	}
}

func TestEmbed32_PropagatesError(t *testing.T) {
	stub := &stubEmbedClient{err: errors.New("upstream down")}
	_, err := Embed32(context.Background(), stub, "hello")
	if err == nil || err.Error() != "upstream down" {
		t.Fatalf("expected upstream error to propagate, got: %v", err)
	}
}

func TestEmbed32_EmptyVector(t *testing.T) {
	stub := &stubEmbedClient{vec: nil}
	got, err := Embed32(context.Background(), stub, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got len=%d", len(got))
	}
}
