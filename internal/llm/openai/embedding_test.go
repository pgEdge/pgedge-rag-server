//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbeddingProvider_Embed(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("expected path /embeddings, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing or incorrect Authorization header")
		}

		resp := embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.1, 0.2, 0.3}, Index: 0},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL))
	provider := NewEmbeddingProvider("test-key", WithEmbeddingClient(client))

	embedding, err := provider.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}

	if len(embedding) != 3 {
		t.Errorf("expected 3 dimensions, got %d", len(embedding))
	}
}

func TestEmbeddingProvider_EmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		resp := embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Embedding: []float32{0.1, 0.2}, Index: 0},
				{Embedding: []float32{0.3, 0.4}, Index: 1},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL))
	provider := NewEmbeddingProvider("test-key", WithEmbeddingClient(client))

	embeddings, err := provider.EmbedBatch(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if len(embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(embeddings))
	}
}

func TestEmbeddingProvider_EmbedBatch_Empty(t *testing.T) {
	provider := NewEmbeddingProvider("test-key")

	embeddings, err := provider.EmbedBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	if embeddings != nil {
		t.Error("expected nil for empty input")
	}
}

func TestEmbeddingProvider_Dimensions(t *testing.T) {
	provider := NewEmbeddingProvider("test-key")
	if provider.Dimensions() != 1536 {
		t.Errorf("expected 1536 dimensions, got %d", provider.Dimensions())
	}

	provider = NewEmbeddingProvider("test-key", WithDimensions(768))
	if provider.Dimensions() != 768 {
		t.Errorf("expected 768 dimensions, got %d", provider.Dimensions())
	}
}

func TestEmbeddingProvider_ModelName(t *testing.T) {
	provider := NewEmbeddingProvider("test-key")
	if provider.ModelName() != defaultEmbeddingModel {
		t.Errorf("expected %s, got %s", defaultEmbeddingModel, provider.ModelName())
	}

	provider = NewEmbeddingProvider("test-key", WithEmbeddingModel("custom-model"))
	if provider.ModelName() != "custom-model" {
		t.Errorf("expected custom-model, got %s", provider.ModelName())
	}
}
