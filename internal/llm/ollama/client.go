//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package ollama provides an Ollama API client for local LLM inference.
package ollama

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pgEdge/pgedge-rag-server/internal/llm/httpclient"
)

const (
	defaultBaseURL        = "http://localhost:11434"
	defaultEmbeddingModel = "nomic-embed-text"
	defaultChatModel      = "llama3.2"
	defaultTimeout        = 120 // Ollama can be slower for large models
)

// Client is an Ollama API client wrapping the shared httpclient.
type Client struct {
	http *httpclient.Client
}

// clientConfig holds configuration for building a Client.
type clientConfig struct {
	baseURL    string
	headers    map[string]string
	httpClient *http.Client
}

// ClientOption configures the client.
type ClientOption func(*clientConfig)

// WithBaseURL sets a custom base URL.
func WithBaseURL(url string) ClientOption {
	return func(cfg *clientConfig) {
		cfg.baseURL = url
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(cfg *clientConfig) {
		cfg.httpClient = client
	}
}

// WithClientHeaders sets custom headers applied to every request.
func WithClientHeaders(headers map[string]string) ClientOption {
	return func(cfg *clientConfig) {
		cfg.headers = headers
	}
}

// NewClient creates a new Ollama client.
func NewClient(opts ...ClientOption) *Client {
	cfg := &clientConfig{
		baseURL: defaultBaseURL,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Build httpclient options
	var hcOpts []httpclient.Option

	hcOpts = append(hcOpts,
		httpclient.WithTimeout(defaultTimeout*time.Second))

	if cfg.httpClient != nil {
		hcOpts = append(hcOpts,
			httpclient.WithHTTPClient(cfg.httpClient))
	}

	if len(cfg.headers) > 0 {
		hcOpts = append(hcOpts, httpclient.WithHeaders(cfg.headers))
	}

	// Ollama is a local server, no auth needed
	hcOpts = append(hcOpts, httpclient.WithAuth(httpclient.NoAuth()))

	return &Client{
		http: httpclient.NewClient(cfg.baseURL, hcOpts...),
	}
}

// parseError extracts error information from an API response.
func parseError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("API error (status %d): failed to read body",
			resp.StatusCode)
	}
	return fmt.Errorf("API error (status %d): %s",
		resp.StatusCode, string(body))
}
