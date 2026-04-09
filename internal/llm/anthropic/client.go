//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package anthropic provides an Anthropic API client.
package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/pgEdge/pgedge-rag-server/internal/llm/httpclient"
)

const (
	defaultBaseURL = "https://api.anthropic.com/v1"
	defaultModel   = "claude-sonnet-4-20250514"
	apiVersion     = "2023-06-01"
)

// Client is an Anthropic API client wrapping the shared httpclient.
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

// WithClientHeaders sets custom headers applied to every request.
func WithClientHeaders(headers map[string]string) ClientOption {
	return func(cfg *clientConfig) {
		cfg.headers = headers
	}
}

// NewClient creates a new Anthropic client.
func NewClient(apiKey string, opts ...ClientOption) *Client {
	cfg := &clientConfig{
		baseURL: defaultBaseURL,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Merge anthropic-version into headers (always required).
	// User-provided headers are applied first; anthropic-version is
	// always present and cannot be overridden by user headers.
	headers := make(map[string]string)
	for k, v := range cfg.headers {
		headers[k] = v
	}
	headers["anthropic-version"] = apiVersion

	var hcOpts []httpclient.Option
	if cfg.httpClient != nil {
		hcOpts = append(hcOpts, httpclient.WithHTTPClient(cfg.httpClient))
	}
	hcOpts = append(hcOpts, httpclient.WithHeaders(headers))
	hcOpts = append(hcOpts, httpclient.WithAuth(
		httpclient.HeaderAuth("x-api-key", apiKey)))

	return &Client{
		http: httpclient.NewClient(cfg.baseURL, hcOpts...),
	}
}

// ErrorResponse represents an Anthropic API error.
type ErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// parseError extracts error information from an API response.
func parseError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("API error (status %d): failed to read body",
			resp.StatusCode)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err != nil {
		return fmt.Errorf("API error (status %d): %s",
			resp.StatusCode, string(body))
	}

	return fmt.Errorf("API error (status %d): %s",
		resp.StatusCode, errResp.Error.Message)
}
