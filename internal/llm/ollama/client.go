//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package ollama provides an Ollama API client for local LLM inference.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultBaseURL        = "http://localhost:11434"
	defaultEmbeddingModel = "nomic-embed-text"
	defaultChatModel      = "llama3.2"
	defaultTimeout        = 120 // Ollama can be slower for large models
)

// Client is an Ollama API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new Ollama client.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: defaultTimeout * time.Second,
		},
		baseURL: defaultBaseURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ClientOption configures the client.
type ClientOption func(*Client)

// WithBaseURL sets a custom base URL.
func WithBaseURL(url string) ClientOption {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithTimeout sets the HTTP timeout.
func WithTimeout(seconds int) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = time.Duration(seconds) * time.Second
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// request makes an HTTP request to the Ollama API.
func (c *Client) request(
	ctx context.Context,
	method, path string,
	body interface{},
) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewReader(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// parseError extracts error information from an API response.
func parseError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("API error (status %d): failed to read body", resp.StatusCode)
	}
	return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
}
