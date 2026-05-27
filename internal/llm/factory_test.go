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
