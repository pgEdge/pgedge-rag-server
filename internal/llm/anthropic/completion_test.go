//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgEdge/pgedge-rag-server/internal/llm"
)

func TestBuildMessages_SystemPrompt(t *testing.T) {
	provider := NewCompletionProvider("test-api-key")

	tests := []struct {
		name           string
		req            llm.CompletionRequest
		expectSystem   string
		expectContains []string
	}{
		{
			name: "with custom system prompt only",
			req: llm.CompletionRequest{
				SystemPrompt: "You are Ellie, a helpful assistant.",
				Messages:     []llm.Message{{Role: "user", Content: "Hello"}},
			},
			expectSystem:   "You are Ellie, a helpful assistant.",
			expectContains: []string{"Ellie"},
		},
		{
			name: "with system prompt and context",
			req: llm.CompletionRequest{
				SystemPrompt: "You are Ellie.",
				Context: []llm.ContextDocument{
					{Content: "Document content here"},
				},
				Messages: []llm.Message{{Role: "user", Content: "Hello"}},
			},
			expectContains: []string{"Ellie", "Document content here"},
		},
		{
			name: "empty system prompt",
			req: llm.CompletionRequest{
				SystemPrompt: "",
				Messages:     []llm.Message{{Role: "user", Content: "Hello"}},
			},
			expectSystem: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages, system := provider.buildMessages(tt.req)

			if tt.expectSystem != "" && system != tt.expectSystem {
				// For exact match tests
				if tt.name == "with custom system prompt only" && system != tt.expectSystem {
					t.Errorf("expected system %q, got %q", tt.expectSystem, system)
				}
			}

			for _, expected := range tt.expectContains {
				if !strings.Contains(system, expected) {
					t.Errorf("system should contain %q, got %q", expected, system)
				}
			}

			// Verify messages are built correctly
			if len(messages) != len(tt.req.Messages) {
				t.Errorf("expected %d messages, got %d", len(tt.req.Messages), len(messages))
			}
		})
	}
}

func TestComplete_SystemPromptInRequest(t *testing.T) {
	// Create a test server that captures the request
	var capturedRequest messagesRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}

		if err := json.Unmarshal(body, &capturedRequest); err != nil {
			t.Fatalf("failed to unmarshal request: %v", err)
		}

		// Return a mock response
		response := messagesResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: "Test response"},
			},
			StopReason: "end_turn",
			Usage: struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			}{
				InputTokens:  100,
				OutputTokens: 10,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	// Create provider with custom client pointing to test server
	client := NewClient("test-api-key", WithBaseURL(server.URL))
	provider := NewCompletionProvider("test-api-key", WithCompletionClient(client))

	customPrompt := "You are Ellie, a custom assistant for pgEdge."

	req := llm.CompletionRequest{
		SystemPrompt: customPrompt,
		Messages:     []llm.Message{{Role: "user", Content: "Hello"}},
	}

	_, err := provider.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	// Verify the system prompt was included in the request
	if !strings.Contains(capturedRequest.System, customPrompt) {
		t.Errorf("API request System should contain %q, got %q",
			customPrompt, capturedRequest.System)
	}
}

func TestComplete_EmptySystemPrompt(t *testing.T) {
	var capturedRequest messagesRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
			return
		}
		if err := json.Unmarshal(body, &capturedRequest); err != nil {
			t.Errorf("failed to unmarshal: %v", err)
			return
		}

		response := messagesResponse{
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "text", Text: "Test response"},
			},
			StopReason: "end_turn",
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient("test-api-key", WithBaseURL(server.URL))
	provider := NewCompletionProvider("test-api-key", WithCompletionClient(client))

	req := llm.CompletionRequest{
		SystemPrompt: "", // Empty
		Messages:     []llm.Message{{Role: "user", Content: "Hello"}},
	}

	_, err := provider.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	// With empty system prompt and no context, system should be empty
	if capturedRequest.System != "" {
		t.Errorf("expected empty system, got %q", capturedRequest.System)
	}
}
