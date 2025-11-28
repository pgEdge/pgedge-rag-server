//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package pipeline provides the RAG pipeline execution and management.
package pipeline

// Info contains basic pipeline information for listing.
type Info struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Message represents a message in the conversation history.
type Message struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// QueryRequest represents a RAG query request.
type QueryRequest struct {
	Query          string    `json:"query"`
	Stream         bool      `json:"stream"`
	TopN           int       `json:"top_n,omitempty"`    // Override default top-N results
	IncludeSources bool      `json:"include_sources"`    // Include source documents (default: false)
	Messages       []Message `json:"messages,omitempty"` // Previous conversation history
}

// QueryResponse represents a non-streaming RAG query response.
type QueryResponse struct {
	Answer     string   `json:"answer"`
	Sources    []Source `json:"sources,omitempty"`
	TokensUsed int      `json:"tokens_used"`
}

// Source represents a source document used in the RAG response.
type Source struct {
	ID      string  `json:"id,omitempty"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// StreamEvent represents a streaming response event.
type StreamEvent struct {
	Type    string   `json:"type"`              // "chunk", "sources", "done", "error"
	Content string   `json:"content,omitempty"` // For "chunk" type
	Sources []Source `json:"sources,omitempty"` // For "sources" type
	Error   string   `json:"error,omitempty"`   // For "error" type
}

// StreamChunk represents a chunk of streaming response from the orchestrator.
type StreamChunk struct {
	Content      string `json:"content,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
}
