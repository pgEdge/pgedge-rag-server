//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package httpclient provides a shared HTTP client for LLM providers.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultTimeout = 60

// AuthFunc applies authentication to an HTTP request.
type AuthFunc func(req *http.Request)

// Client is a shared HTTP client for LLM provider APIs.
type Client struct {
	httpClient *http.Client
	baseURL    string
	headers    map[string]string
	authFn     AuthFunc
}

// Option configures the client.
type Option func(*Client)

// NewClient creates a new HTTP client with the given base URL.
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: defaultTimeout * time.Second,
		},
		baseURL: baseURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithAuth sets the authentication function.
func WithAuth(fn AuthFunc) Option {
	return func(c *Client) {
		c.authFn = fn
	}
}

// WithHeaders sets custom headers applied to every request.
func WithHeaders(headers map[string]string) Option {
	return func(c *Client) {
		c.headers = headers
	}
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// WithHTTPClient sets a custom net/http client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		c.httpClient = client
	}
}

// Do executes an HTTP request. If body is non-nil, it is sent as
// the request body and Content-Type is set to application/json.
// Custom headers are applied first, then auth, so auth headers
// take precedence.
func (c *Client) Do(
	ctx context.Context,
	method, path string,
	body []byte,
) (*http.Response, error) {
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	var req *http.Request
	var err error
	if bodyReader != nil {
		req, err = http.NewRequestWithContext(ctx, method,
			c.baseURL+path, bodyReader)
	} else {
		req, err = http.NewRequestWithContext(ctx, method,
			c.baseURL+path, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Apply custom headers first
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	// Apply auth last (takes precedence over custom headers)
	if c.authFn != nil {
		c.authFn(req)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// DoJSON marshals reqBody to JSON, sends the request via Do, and
// unmarshals the response into respBody. Returns an error if the
// response status is not 2xx.
func (c *Client) DoJSON(
	ctx context.Context,
	method, path string,
	reqBody interface{},
	respBody interface{},
) error {
	var body []byte
	if reqBody != nil {
		var err error
		body, err = json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf(
				"failed to marshal request body: %w", err)
		}
	}

	resp, err := c.Do(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf(
			"failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d: %s",
			resp.StatusCode, string(respData))
	}

	if respBody != nil && len(respData) > 0 {
		if err := json.Unmarshal(respData, respBody); err != nil {
			return fmt.Errorf(
				"failed to unmarshal response: %w", err)
		}
	}

	return nil
}

// DoStream sends a request via Do and returns the response body as
// an io.ReadCloser for streaming. Returns an error if the response
// status is not 2xx. The caller is responsible for closing the
// returned ReadCloser.
func (c *Client) DoStream(
	ctx context.Context,
	method, path string,
	body []byte,
) (io.ReadCloser, error) {
	resp, err := c.Do(ctx, method, path, body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		respData, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s",
			resp.StatusCode, string(respData))
	}

	return resp.Body, nil
}
