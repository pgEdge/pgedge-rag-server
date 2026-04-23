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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgEdge/pgedge-rag-server/internal/llm"
)

func TestCompletionProvider_Complete(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("key") != "test-key" {
				t.Errorf("expected key=test-key, got %s",
					r.URL.Query().Get("key"))
			}

			var req generateContentRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			if len(req.Contents) == 0 {
				t.Fatal("expected non-empty contents")
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
                "candidates": [{
                    "content": {
                        "parts": [{"text": "Hello from Gemini"}],
                        "role": "model"
                    },
                    "finishReason": "STOP"
                }],
                "usageMetadata": {
                    "promptTokenCount": 10,
                    "candidatesTokenCount": 5,
                    "totalTokenCount": 15
                }
            }`))
		}),
	)
	defer server.Close()

	p := NewCompletionProvider("test-key",
		WithCompletionBaseURL(server.URL))

	resp, err := p.Complete(context.Background(),
		llm.CompletionRequest{
			Messages: []llm.Message{
				{Role: "user", Content: "Hello"},
			},
		})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
	if resp.Content != "Hello from Gemini" {
		t.Errorf("expected 'Hello from Gemini', got '%s'",
			resp.Content)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d",
			resp.Usage.TotalTokens)
	}
}

func TestCompletionProvider_Complete_WithSystemPrompt(
	t *testing.T,
) {
	var capturedReq generateContentRequest
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&capturedReq)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
                "candidates": [{
                    "content": {
                        "parts": [{"text": "response"}],
                        "role": "model"
                    },
                    "finishReason": "STOP"
                }],
                "usageMetadata": {
                    "promptTokenCount": 10,
                    "candidatesTokenCount": 5,
                    "totalTokenCount": 15
                }
            }`))
		}),
	)
	defer server.Close()

	p := NewCompletionProvider("test-key",
		WithCompletionBaseURL(server.URL))

	_, err := p.Complete(context.Background(),
		llm.CompletionRequest{
			SystemPrompt: "You are helpful",
			Messages: []llm.Message{
				{Role: "user", Content: "Hi"},
			},
			Context: []llm.ContextDocument{
				{Content: "doc1", Source: "src1"},
			},
		})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if capturedReq.SystemInstruction == nil {
		t.Fatal("expected system instruction to be set")
	}
	if len(capturedReq.SystemInstruction.Parts) == 0 {
		t.Fatal("expected system instruction parts")
	}
}

func TestCompletionProvider_Complete_ExplicitZeroTemperature(
	t *testing.T,
) {
	var rawBody []byte
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawBody, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
                "candidates": [{
                    "content": {
                        "parts": [{"text": "ok"}],
                        "role": "model"
                    },
                    "finishReason": "STOP"
                }],
                "usageMetadata": {
                    "promptTokenCount": 1,
                    "candidatesTokenCount": 1,
                    "totalTokenCount": 2
                }
            }`))
		}),
	)
	defer server.Close()

	p := NewCompletionProvider("test-key",
		WithCompletionBaseURL(server.URL),
		WithTemperature(0.0))

	_, err := p.Complete(context.Background(),
		llm.CompletionRequest{
			Temperature: -1,
			Messages: []llm.Message{
				{Role: "user", Content: "Hi"},
			},
		})
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(rawBody, &decoded); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	gc, ok := decoded["generationConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected generationConfig in request, got %v",
			decoded)
	}
	temp, present := gc["temperature"]
	if !present {
		t.Fatal("expected temperature field to be present when " +
			"explicitly set to 0.0")
	}
	if f, _ := temp.(float64); f != 0.0 {
		t.Errorf("expected temperature=0, got %v", temp)
	}
}

func TestCompletionProvider_ModelName(t *testing.T) {
	p := NewCompletionProvider("key")
	if p.ModelName() != defaultChatModel {
		t.Errorf("expected %s, got %s",
			defaultChatModel, p.ModelName())
	}

	p2 := NewCompletionProvider("key",
		WithCompletionModel("gemini-1.5-pro"))
	if p2.ModelName() != "gemini-1.5-pro" {
		t.Errorf("expected gemini-1.5-pro, got %s",
			p2.ModelName())
	}
}
