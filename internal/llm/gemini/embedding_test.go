//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddingProvider_Embed(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("key") != "test-key" {
				t.Errorf("expected key=test-key, got %s",
					r.URL.Query().Get("key"))
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
                "embeddings": [
                    {"values": [0.1, 0.2, 0.3]}
                ]
            }`))
		}),
	)
	defer server.Close()

	p := NewEmbeddingProvider("test-key",
		WithEmbeddingBaseURL(server.URL))

	embedding, err := p.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed failed: %v", err)
	}
	if len(embedding) != 3 {
		t.Fatalf("expected 3 dimensions, got %d",
			len(embedding))
	}
	if embedding[0] != 0.1 {
		t.Errorf("expected 0.1, got %f", embedding[0])
	}
}

func TestEmbeddingProvider_EmbedBatch(t *testing.T) {
	callCount := 0
	var capturedReq batchEmbedContentsRequest
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			_ = json.NewDecoder(r.Body).Decode(&capturedReq)
			if !strings.Contains(
				r.URL.Path, ":batchEmbedContents",
			) {
				t.Errorf(
					"expected batchEmbedContents endpoint, got %s",
					r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
                "embeddings": [
                    {"values": [0.1, 0.2, 0.3]},
                    {"values": [0.4, 0.5, 0.6]}
                ]
            }`))
		}),
	)
	defer server.Close()

	p := NewEmbeddingProvider("test-key",
		WithEmbeddingBaseURL(server.URL))

	embeddings, err := p.EmbedBatch(context.Background(),
		[]string{"hello", "world"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(embeddings) != 2 {
		t.Fatalf("expected 2 embeddings, got %d",
			len(embeddings))
	}
	if callCount != 1 {
		t.Errorf("expected 1 API call, got %d", callCount)
	}
	if len(capturedReq.Requests) != 2 {
		t.Fatalf("expected 2 sub-requests, got %d",
			len(capturedReq.Requests))
	}
	if capturedReq.Requests[0].Content.Parts[0].Text != "hello" {
		t.Errorf("expected 'hello' in first sub-request, got %q",
			capturedReq.Requests[0].Content.Parts[0].Text)
	}
	if capturedReq.Requests[1].Content.Parts[0].Text != "world" {
		t.Errorf("expected 'world' in second sub-request, got %q",
			capturedReq.Requests[1].Content.Parts[0].Text)
	}
	if embeddings[0][0] != 0.1 || embeddings[1][0] != 0.4 {
		t.Errorf("embeddings returned in wrong order: %v",
			embeddings)
	}
}

func TestEmbeddingProvider_EmbedBatch_MismatchedResponse(
	t *testing.T,
) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
                "embeddings": [
                    {"values": [0.1, 0.2, 0.3]}
                ]
            }`))
		}),
	)
	defer server.Close()

	p := NewEmbeddingProvider("test-key",
		WithEmbeddingBaseURL(server.URL))

	_, err := p.EmbedBatch(context.Background(),
		[]string{"hello", "world"})
	if err == nil {
		t.Fatal("expected error on mismatched count, got nil")
	}
}

func TestEmbeddingProvider_EmbedBatch_Empty(t *testing.T) {
	p := NewEmbeddingProvider("test-key")
	embeddings, err := p.EmbedBatch(
		context.Background(), []string{})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if embeddings != nil {
		t.Error("expected nil for empty input")
	}
}

func TestEmbeddingProvider_Dimensions(t *testing.T) {
	p := NewEmbeddingProvider("test-key")
	if p.Dimensions() != 768 {
		t.Errorf("expected 768, got %d", p.Dimensions())
	}
}

func TestEmbeddingProvider_ModelName(t *testing.T) {
	p := NewEmbeddingProvider("test-key")
	if p.ModelName() != defaultEmbeddingModel {
		t.Errorf("expected %s, got %s",
			defaultEmbeddingModel, p.ModelName())
	}
}

func TestEmbeddingProvider_EmbedRequest(t *testing.T) {
	var capturedReq batchEmbedContentsRequest
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedReq)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
                "embeddings": [{"values": [0.1]}]
            }`))
		}),
	)
	defer server.Close()

	p := NewEmbeddingProvider("test-key",
		WithEmbeddingBaseURL(server.URL))
	_, _ = p.Embed(context.Background(), "test text")

	if len(capturedReq.Requests) != 1 {
		t.Fatalf("expected 1 sub-request, got %d",
			len(capturedReq.Requests))
	}
	sub := capturedReq.Requests[0]
	if sub.Model != "models/"+defaultEmbeddingModel {
		t.Errorf("expected model ref %q, got %q",
			"models/"+defaultEmbeddingModel, sub.Model)
	}
	if len(sub.Content.Parts) == 0 {
		t.Fatal("expected Content.Parts to be non-empty")
	}
	if sub.Content.Parts[0].Text != "test text" {
		t.Errorf("expected 'test text' in request, got '%s'",
			sub.Content.Parts[0].Text)
	}
}
