//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package voyage provides a Voyage AI embedding client.
package voyage

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pgEdge/pgedge-rag-server/internal/llm/httpclient"
)

const (
	defaultBaseURL = "https://api.voyageai.com/v1"
	defaultModel   = "voyage-3"
	defaultTimeout = 60
)

// Client is a Voyage AI API client wrapping the shared httpclient.
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

// NewClient creates a new Voyage AI client.
func NewClient(apiKey string, opts ...ClientOption) *Client {
	cfg := &clientConfig{
		baseURL: defaultBaseURL,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Build httpclient options. Apply WithHTTPClient before
	// WithTimeout so the timeout is set on the resulting client
	// rather than being discarded when a custom client replaces it.
	var hcOpts []httpclient.Option

	if cfg.httpClient != nil {
		hcOpts = append(hcOpts,
			httpclient.WithHTTPClient(cfg.httpClient))
	}

	hcOpts = append(hcOpts,
		httpclient.WithTimeout(defaultTimeout*time.Second))

	if len(cfg.headers) > 0 {
		hcOpts = append(hcOpts, httpclient.WithHeaders(cfg.headers))
	}

	// Use BearerAuth when apiKey is provided, NoAuth otherwise
	if apiKey != "" {
		hcOpts = append(hcOpts, httpclient.WithAuth(
			httpclient.BearerAuth(apiKey)))
	} else {
		hcOpts = append(hcOpts, httpclient.WithAuth(
			httpclient.NoAuth()))
	}

	return &Client{
		http: httpclient.NewClient(cfg.baseURL, hcOpts...),
	}
}

// ErrorResponse represents a Voyage API error.
type ErrorResponse struct {
	Detail string `json:"detail"`
}

// parseError extracts error information from an API response.
// It reads from resp.Body, which must not have been consumed or
// closed. The caller remains responsible for closing resp.Body.
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
		resp.StatusCode, errResp.Detail)
}
