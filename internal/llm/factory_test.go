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
	"time"

	llmlib "github.com/pgEdge/pgedge-go-llm-lib/llm"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

func TestNewEmbeddingClient_OpenAI(t *testing.T) {
	keys := &config.LoadedKeys{OpenAI: "sk-test"}
	c, err := NewEmbeddingClient("openai", "text-embedding-3-small", "", nil, keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.Provider() != "openai" {
		t.Errorf("Provider()=%q, want openai", c.Provider())
	}
	if c.Model() != "text-embedding-3-small" {
		t.Errorf("Model()=%q, want text-embedding-3-small", c.Model())
	}
}

func TestNewEmbeddingClient_OpenAI_BaseURLSubstitutesForKey(t *testing.T) {
	keys := &config.LoadedKeys{}
	c, err := NewEmbeddingClient(
		"openai", "nomic-embed-text",
		"http://localhost:1234/v1", nil, keys,
	)
	if err != nil {
		t.Fatalf("baseURL should satisfy the 'API key required' check: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewEmbeddingClient_OpenAI_NoKeyNoBaseURL(t *testing.T) {
	keys := &config.LoadedKeys{}
	_, err := NewEmbeddingClient("openai", "text-embedding-3-small", "", nil, keys)
	if err == nil {
		t.Fatal("expected error when no key and no baseURL")
	}
	if !strings.Contains(err.Error(), "OpenAI") {
		t.Errorf("error should name OpenAI: %v", err)
	}
}

func TestNewEmbeddingClient_VoyageMissingKey(t *testing.T) {
	keys := &config.LoadedKeys{}
	_, err := NewEmbeddingClient("voyage", "voyage-3", "", nil, keys)
	if err == nil || !strings.Contains(err.Error(), "Voyage") {
		t.Errorf("expected Voyage key error, got %v", err)
	}
}

func TestNewEmbeddingClient_GeminiMissingKey(t *testing.T) {
	keys := &config.LoadedKeys{}
	_, err := NewEmbeddingClient("gemini", "text-embedding-004", "", nil, keys)
	if err == nil || !strings.Contains(err.Error(), "Gemini") {
		t.Errorf("expected Gemini key error, got %v", err)
	}
}

func TestNewEmbeddingClient_Ollama_NoKeyOK(t *testing.T) {
	keys := &config.LoadedKeys{}
	c, err := NewEmbeddingClient(
		"ollama", "nomic-embed-text",
		"http://localhost:11434", nil, keys,
	)
	if err != nil {
		t.Fatalf("Ollama should not require a key: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewEmbeddingClient_AnthropicRejected(t *testing.T) {
	keys := &config.LoadedKeys{Anthropic: "sk-test"}
	_, err := NewEmbeddingClient("anthropic", "", "", nil, keys)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "embedding") {
		t.Errorf("Anthropic should be rejected for embeddings, got: %v", err)
	}
}

func TestNewEmbeddingClient_UnknownProvider(t *testing.T) {
	keys := &config.LoadedKeys{}
	_, err := NewEmbeddingClient("nonesuch", "", "", nil, keys)
	if err == nil || !strings.Contains(err.Error(), "nonesuch") {
		t.Errorf("expected error naming the unknown provider, got %v", err)
	}
}

func TestNewEmbeddingClient_LowerCasesProviderName(t *testing.T) {
	keys := &config.LoadedKeys{OpenAI: "sk-test"}
	c, err := NewEmbeddingClient("OpenAI", "text-embedding-3-small", "", nil, keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Provider() != "openai" {
		t.Errorf("provider should be lower-cased; got %q", c.Provider())
	}
}

func TestNewEmbeddingClient_CustomHeadersAccepted(t *testing.T) {
	keys := &config.LoadedKeys{OpenAI: "sk-test"}
	headers := map[string]string{"X-Trace-Id": "abc123"}
	_, err := NewEmbeddingClient(
		"openai", "text-embedding-3-small", "", headers, keys,
	)
	if err != nil {
		t.Fatalf("custom headers should not cause an error: %v", err)
	}
}

func TestNewCompletionClient_Anthropic(t *testing.T) {
	keys := &config.LoadedKeys{Anthropic: "sk-test"}
	c, err := NewCompletionClient(
		"anthropic", "claude-sonnet-4-20250514", "", nil, keys,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Provider() != "anthropic" {
		t.Errorf("Provider()=%q, want anthropic", c.Provider())
	}
}

func TestNewCompletionClient_OpenAI(t *testing.T) {
	keys := &config.LoadedKeys{OpenAI: "sk-test"}
	c, err := NewCompletionClient("openai", "gpt-4o", "", nil, keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewCompletionClient_OpenAI_BaseURLSubstitutesForKey(t *testing.T) {
	keys := &config.LoadedKeys{}
	c, err := NewCompletionClient(
		"openai", "local-model",
		"http://localhost:1234/v1", nil, keys,
	)
	if err != nil {
		t.Fatalf("baseURL should satisfy the 'API key required' check: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewCompletionClient_AnthropicMissingKey(t *testing.T) {
	keys := &config.LoadedKeys{}
	_, err := NewCompletionClient(
		"anthropic", "claude-sonnet-4-20250514", "", nil, keys,
	)
	if err == nil || !strings.Contains(err.Error(), "Anthropic") {
		t.Errorf("expected Anthropic key error, got %v", err)
	}
}

func TestNewCompletionClient_GeminiMissingKey(t *testing.T) {
	keys := &config.LoadedKeys{}
	_, err := NewCompletionClient("gemini", "gemini-1.5-pro", "", nil, keys)
	if err == nil || !strings.Contains(err.Error(), "Gemini") {
		t.Errorf("expected Gemini key error, got %v", err)
	}
}

func TestNewCompletionClient_Ollama_NoKeyOK(t *testing.T) {
	keys := &config.LoadedKeys{}
	c, err := NewCompletionClient(
		"ollama", "llama3",
		"http://localhost:11434", nil, keys,
	)
	if err != nil {
		t.Fatalf("Ollama should not require a key: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewCompletionClient_VoyageRejected(t *testing.T) {
	keys := &config.LoadedKeys{Voyage: "vk-test"}
	_, err := NewCompletionClient("voyage", "", "", nil, keys)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "completion") {
		t.Errorf("Voyage should be rejected for completion, got: %v", err)
	}
}

func TestNewCompletionClient_UnknownProvider(t *testing.T) {
	keys := &config.LoadedKeys{}
	_, err := NewCompletionClient("nonesuch", "", "", nil, keys)
	if err == nil || !strings.Contains(err.Error(), "nonesuch") {
		t.Errorf("expected error naming the unknown provider, got %v", err)
	}
}

// Nil-keys regression tests: passing a nil *config.LoadedKeys must
// surface as a normal validation error, not a nil-pointer panic.
func TestNewEmbeddingClient_NilKeys(t *testing.T) {
	_, err := NewEmbeddingClient("openai", "text-embedding-3-small", "", nil, nil)
	if err == nil {
		t.Fatal("expected error when keys is nil and no baseURL")
	}
	if !strings.Contains(err.Error(), "OpenAI") {
		t.Errorf("error should name OpenAI: %v", err)
	}
}

func TestNewCompletionClient_NilKeys(t *testing.T) {
	_, err := NewCompletionClient("anthropic", "claude-sonnet-4-20250514", "", nil, nil)
	if err == nil {
		t.Fatal("expected error when keys is nil")
	}
	if !strings.Contains(err.Error(), "Anthropic") {
		t.Errorf("error should name Anthropic: %v", err)
	}
}

func TestNewRerankClient_Voyage(t *testing.T) {
	keys := &config.LoadedKeys{Voyage: "vk-test"}
	c, err := NewRerankClient("voyage", "rerank-2", "", nil, keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if c.Provider() != "voyage" {
		t.Errorf("Provider()=%q, want voyage", c.Provider())
	}
	if c.Model() != "rerank-2" {
		t.Errorf("Model()=%q, want rerank-2", c.Model())
	}
}

func TestNewRerankClient_VoyageMissingKey(t *testing.T) {
	keys := &config.LoadedKeys{}
	_, err := NewRerankClient("voyage", "rerank-2", "", nil, keys)
	if err == nil || !strings.Contains(err.Error(), "Voyage") {
		t.Errorf("expected Voyage key error, got %v", err)
	}
}

func TestNewRerankClient_LowerCasesProviderName(t *testing.T) {
	keys := &config.LoadedKeys{Voyage: "vk-test"}
	c, err := NewRerankClient("Voyage", "rerank-2", "", nil, keys)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Provider() != "voyage" {
		t.Errorf("provider should be lower-cased; got %q", c.Provider())
	}
}

func TestNewRerankClient_RejectsNonVoyageProviders(t *testing.T) {
	rejected := []string{"anthropic", "openai", "gemini", "ollama"}
	for _, p := range rejected {
		t.Run(p, func(t *testing.T) {
			keys := &config.LoadedKeys{
				Anthropic: "sk-test", OpenAI: "sk-test", Gemini: "sk-test",
			}
			_, err := NewRerankClient(p, "some-model", "", nil, keys)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "reranking") {
				t.Errorf("%s should be rejected for reranking, got: %v", p, err)
			}
		})
	}
}

func TestNewRerankClient_UnknownProvider(t *testing.T) {
	keys := &config.LoadedKeys{}
	_, err := NewRerankClient("nonesuch", "", "", nil, keys)
	if err == nil || !strings.Contains(err.Error(), "reranking") {
		t.Errorf("expected error naming reranking as unsupported, got %v", err)
	}
}

func TestNewRerankClient_CustomHeadersAccepted(t *testing.T) {
	keys := &config.LoadedKeys{Voyage: "vk-test"}
	headers := map[string]string{"X-Trace-Id": "abc123"}
	_, err := NewRerankClient("voyage", "rerank-2", "", headers, keys)
	if err != nil {
		t.Fatalf("custom headers should not cause an error: %v", err)
	}
}

func TestNewRerankClient_NilKeys(t *testing.T) {
	_, err := NewRerankClient("voyage", "rerank-2", "", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "Voyage") {
		t.Errorf("expected Voyage key error, got %v", err)
	}
}

func TestWithOptions_AppliesTimeouts(t *testing.T) {
	got := withOptions(llmlib.Options{Model: "x"}, []ClientOption{
		WithRequestTimeout(90 * time.Second),
		WithPerAttemptTimeout(30 * time.Second),
	})
	if got.RequestTimeout != 90*time.Second {
		t.Errorf("RequestTimeout = %v, want 90s", got.RequestTimeout)
	}
	if got.PerAttemptTimeout != 30*time.Second {
		t.Errorf("PerAttemptTimeout = %v, want 30s", got.PerAttemptTimeout)
	}
	if got.Model != "x" {
		t.Errorf("base Options not preserved: Model = %q", got.Model)
	}
}

func TestWithOptions_NoOptionsLeavesTimeoutsZero(t *testing.T) {
	got := withOptions(llmlib.Options{}, nil)
	if got.RequestTimeout != 0 || got.PerAttemptTimeout != 0 {
		t.Errorf("expected zero timeouts, got request=%v per-attempt=%v",
			got.RequestTimeout, got.PerAttemptTimeout)
	}
}
