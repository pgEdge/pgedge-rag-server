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

	"github.com/pgEdge/pgedge-rag-server/internal/llm"
)

func TestCompletionProvider_Complete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected path /chat/completions, got %s", r.URL.Path)
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if req.Stream {
			t.Error("expected stream to be false")
		}

		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Content string `json:"content"`
					}{Content: "Hello!"},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			}{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL))
	provider := NewCompletionProvider("test-key", WithCompletionClient(client))

	req := llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "Hi there"},
		},
	}

	resp, err := provider.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if resp.Content != "Hello!" {
		t.Errorf("expected 'Hello!', got %s", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("expected 'stop', got %s", resp.FinishReason)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("expected 15 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

func TestCompletionProvider_Complete_WithContext(t *testing.T) {
	var receivedMessages []chatMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}
		receivedMessages = req.Messages

		resp := chatResponse{
			Choices: []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Message: struct {
						Content string `json:"content"`
					}{Content: "Response"},
					FinishReason: "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL))
	provider := NewCompletionProvider("test-key", WithCompletionClient(client))

	req := llm.CompletionRequest{
		SystemPrompt: "You are a helpful assistant.",
		Context: []llm.ContextDocument{
			{Content: "Document 1", Source: "test.txt"},
		},
		Messages: []llm.Message{
			{Role: "user", Content: "What's in the document?"},
		},
	}

	_, err := provider.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	// Should have system prompt, context, and user message
	if len(receivedMessages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(receivedMessages))
	}
	if receivedMessages[0].Role != "system" {
		t.Errorf("expected first message role 'system', got %s", receivedMessages[0].Role)
	}
}

func TestCompletionProvider_ModelName(t *testing.T) {
	provider := NewCompletionProvider("test-key")
	if provider.ModelName() != defaultChatModel {
		t.Errorf("expected %s, got %s", defaultChatModel, provider.ModelName())
	}

	provider = NewCompletionProvider("test-key", WithCompletionModel("gpt-4"))
	if provider.ModelName() != "gpt-4" {
		t.Errorf("expected gpt-4, got %s", provider.ModelName())
	}
}

func TestCompletionProvider_Options(t *testing.T) {
	provider := NewCompletionProvider(
		"test-key",
		WithMaxTokens(1000),
		WithTemperature(0.5),
	)

	if provider.maxTokens != 1000 {
		t.Errorf("expected maxTokens 1000, got %d", provider.maxTokens)
	}
	if provider.temperature != 0.5 {
		t.Errorf("expected temperature 0.5, got %f", provider.temperature)
	}
}
