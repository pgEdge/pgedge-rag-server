//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package factory

import (
	"testing"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

func TestNewEmbeddingProvider_OpenAI(t *testing.T) {
	keys := &config.LoadedKeys{OpenAI: "test-key"}

	provider, err := NewEmbeddingProvider("openai", "", keys)
	if err != nil {
		t.Fatalf("NewEmbeddingProvider failed: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewEmbeddingProvider_OpenAI_NoKey(t *testing.T) {
	keys := &config.LoadedKeys{}

	_, err := NewEmbeddingProvider("openai", "", keys)
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestNewEmbeddingProvider_Voyage(t *testing.T) {
	keys := &config.LoadedKeys{Voyage: "test-key"}

	provider, err := NewEmbeddingProvider("voyage", "", keys)
	if err != nil {
		t.Fatalf("NewEmbeddingProvider failed: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewEmbeddingProvider_Ollama(t *testing.T) {
	keys := &config.LoadedKeys{}

	provider, err := NewEmbeddingProvider("ollama", "", keys)
	if err != nil {
		t.Fatalf("NewEmbeddingProvider failed: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewEmbeddingProvider_Anthropic(t *testing.T) {
	keys := &config.LoadedKeys{Anthropic: "test-key"}

	_, err := NewEmbeddingProvider("anthropic", "", keys)
	if err == nil {
		t.Fatal("expected error for Anthropic (no embedding API)")
	}
}

func TestNewEmbeddingProvider_Unknown(t *testing.T) {
	keys := &config.LoadedKeys{}

	_, err := NewEmbeddingProvider("unknown", "", keys)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestNewEmbeddingProvider_CaseInsensitive(t *testing.T) {
	keys := &config.LoadedKeys{OpenAI: "test-key"}

	provider, err := NewEmbeddingProvider("OpenAI", "", keys)
	if err != nil {
		t.Fatalf("NewEmbeddingProvider failed: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewCompletionProvider_OpenAI(t *testing.T) {
	keys := &config.LoadedKeys{OpenAI: "test-key"}

	provider, err := NewCompletionProvider("openai", "", keys)
	if err != nil {
		t.Fatalf("NewCompletionProvider failed: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewCompletionProvider_Anthropic(t *testing.T) {
	keys := &config.LoadedKeys{Anthropic: "test-key"}

	provider, err := NewCompletionProvider("anthropic", "", keys)
	if err != nil {
		t.Fatalf("NewCompletionProvider failed: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewCompletionProvider_Ollama(t *testing.T) {
	keys := &config.LoadedKeys{}

	provider, err := NewCompletionProvider("ollama", "", keys)
	if err != nil {
		t.Fatalf("NewCompletionProvider failed: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewCompletionProvider_Voyage(t *testing.T) {
	keys := &config.LoadedKeys{Voyage: "test-key"}

	_, err := NewCompletionProvider("voyage", "", keys)
	if err == nil {
		t.Fatal("expected error for Voyage (no completion API)")
	}
}

func TestNewCompletionProvider_Unknown(t *testing.T) {
	keys := &config.LoadedKeys{}

	_, err := NewCompletionProvider("unknown", "", keys)
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestNewCompletionProvider_WithModel(t *testing.T) {
	keys := &config.LoadedKeys{OpenAI: "test-key"}

	provider, err := NewCompletionProvider("openai", "gpt-4", keys)
	if err != nil {
		t.Fatalf("NewCompletionProvider failed: %v", err)
	}
	if provider.ModelName() != "gpt-4" {
		t.Errorf("expected model gpt-4, got %s", provider.ModelName())
	}
}
