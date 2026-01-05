//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package llm provides interfaces and implementations for LLM providers.
package llm

import (
	"context"
	"fmt"
	"strings"
)

// EmbeddingProvider generates vector embeddings from text.
type EmbeddingProvider interface {
	// Embed generates an embedding vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts.
	// Returns embeddings in the same order as input texts.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimensionality of embeddings produced.
	Dimensions() int

	// ModelName returns the name of the model being used.
	ModelName() string
}

// CompletionProvider generates text completions using an LLM.
type CompletionProvider interface {
	// Complete generates a completion for the given prompt.
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)

	// CompleteStream generates a streaming completion.
	// The returned channel will receive response chunks until completion,
	// then be closed. Errors are returned via the error channel.
	CompleteStream(
		ctx context.Context,
		req CompletionRequest,
	) (<-chan StreamChunk, <-chan error)

	// ModelName returns the name of the model being used.
	ModelName() string
}

// CompletionRequest represents a request to an LLM for completion.
type CompletionRequest struct {
	// SystemPrompt is the system-level instruction for the model.
	SystemPrompt string

	// Messages is the conversation history.
	Messages []Message

	// MaxTokens is the maximum number of tokens to generate.
	// If 0, uses the provider's default.
	MaxTokens int

	// Temperature controls randomness (0.0 = deterministic, 1.0+ = creative).
	// If negative, uses the provider's default.
	Temperature float64

	// Context contains retrieved documents to include in the prompt.
	Context []ContextDocument
}

// Message represents a message in the conversation.
type Message struct {
	Role    string // "user", "assistant", or "system"
	Content string
}

// ContextDocument represents a retrieved document for RAG.
type ContextDocument struct {
	Content  string
	Source   string
	Score    float64
	Metadata map[string]interface{}
}

// CompletionResponse represents a non-streaming completion response.
type CompletionResponse struct {
	Content      string
	FinishReason string
	Usage        TokenUsage
}

// StreamChunk represents a chunk of a streaming response.
type StreamChunk struct {
	Content      string
	FinishReason string // Empty until the final chunk
	Usage        *TokenUsage
}

// TokenUsage represents token consumption for a request.
type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ProviderConfig contains common configuration for all providers.
type ProviderConfig struct {
	APIKey     string
	BaseURL    string
	Model      string
	Timeout    int // Timeout in seconds
	MaxRetries int
}

// Error types for LLM operations.
type Error struct {
	Code       string
	Message    string
	StatusCode int
	Retryable  bool
}

func (e *Error) Error() string {
	return e.Message
}

// Common error codes
const (
	ErrCodeRateLimit    = "rate_limit"
	ErrCodeInvalidKey   = "invalid_api_key"
	ErrCodeQuotaExceed  = "quota_exceeded"
	ErrCodeModelError   = "model_error"
	ErrCodeTimeout      = "timeout"
	ErrCodeNetworkError = "network_error"
)

// IsRetryable returns true if the error can be retried.
func IsRetryable(err error) bool {
	if e, ok := err.(*Error); ok {
		return e.Retryable
	}
	return false
}

// FormatContext formats context documents for inclusion in an LLM prompt.
// This provides a consistent format across all completion providers.
func FormatContext(docs []ContextDocument) string {
	var sb strings.Builder
	sb.WriteString("Use the following context to answer the question:\n\n")

	for i, doc := range docs {
		sb.WriteString(fmt.Sprintf("--- Document %d", i+1))
		if doc.Source != "" {
			sb.WriteString(fmt.Sprintf(" (Source: %s)", doc.Source))
		}
		sb.WriteString(" ---\n")
		sb.WriteString(doc.Content)
		sb.WriteString("\n\n")
	}

	return sb.String()
}
