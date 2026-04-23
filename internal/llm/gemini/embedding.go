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
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pgEdge/pgedge-rag-server/internal/llm"
	"github.com/pgEdge/pgedge-rag-server/internal/llm/httpclient"
)

const (
	defaultEmbeddingModel = "text-embedding-004"
)

// EmbeddingProvider implements llm.EmbeddingProvider.
type EmbeddingProvider struct {
	client     *httpclient.Client
	model      string
	dimensions int
}

// embeddingConfig holds configuration for building an
// EmbeddingProvider.
type embeddingConfig struct {
	model      string
	baseURL    string
	dimensions int
	headers    map[string]string
}

// NewEmbeddingProvider creates a new Gemini embedding provider.
func NewEmbeddingProvider(
	apiKey string, opts ...EmbeddingOption,
) *EmbeddingProvider {
	cfg := &embeddingConfig{
		baseURL: defaultBaseURL,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	httpOpts := []httpclient.Option{
		httpclient.WithAuth(
			httpclient.QueryParamAuth("key", apiKey)),
		httpclient.WithTimeout(
			time.Duration(defaultTimeout) * time.Second),
	}
	if len(cfg.headers) > 0 {
		httpOpts = append(httpOpts,
			httpclient.WithHeaders(cfg.headers))
	}

	p := &EmbeddingProvider{
		client: httpclient.NewClient(
			cfg.baseURL, httpOpts...),
		model:      defaultEmbeddingModel,
		dimensions: 768,
	}
	if cfg.model != "" {
		p.model = cfg.model
	}
	if cfg.dimensions > 0 {
		p.dimensions = cfg.dimensions
	}
	return p
}

// EmbeddingOption configures the embedding provider.
type EmbeddingOption func(*embeddingConfig)

// WithEmbeddingModel sets the embedding model.
func WithEmbeddingModel(model string) EmbeddingOption {
	return func(cfg *embeddingConfig) {
		cfg.model = model
	}
}

// WithEmbeddingBaseURL sets a custom base URL.
func WithEmbeddingBaseURL(url string) EmbeddingOption {
	return func(cfg *embeddingConfig) {
		cfg.baseURL = url
	}
}

// WithEmbeddingDimensions sets the expected dimensions.
func WithEmbeddingDimensions(dims int) EmbeddingOption {
	return func(cfg *embeddingConfig) {
		cfg.dimensions = dims
	}
}

// WithEmbeddingHeaders sets custom headers.
func WithEmbeddingHeaders(
	headers map[string]string,
) EmbeddingOption {
	return func(cfg *embeddingConfig) {
		cfg.headers = headers
	}
}

// Gemini embedding API types

type embedContentRequest struct {
	Model   string  `json:"model,omitempty"`
	Content content `json:"content"`
}

type embedContentResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

type batchEmbedContentsRequest struct {
	Requests []embedContentRequest `json:"requests"`
}

type batchEmbedContentsResponse struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
}

// Embed generates an embedding for a single text.
func (p *EmbeddingProvider) Embed(
	ctx context.Context, text string,
) ([]float32, error) {
	embeddings, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts in a single
// request using Gemini's batchEmbedContents endpoint.
func (p *EmbeddingProvider) EmbedBatch(
	ctx context.Context,
	texts []string,
) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// The API requires each sub-request to reference a fully
	// qualified model name in the form "models/<model>".
	modelRef := "models/" + p.model
	requests := make([]embedContentRequest, len(texts))
	for i, text := range texts {
		requests[i] = embedContentRequest{
			Model: modelRef,
			Content: content{
				Parts: []part{{Text: text}},
			},
		}
	}

	reqBody := batchEmbedContentsRequest{Requests: requests}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to marshal request: %w", err)
	}

	path := fmt.Sprintf(
		"/v1beta/models/%s:batchEmbedContents", p.model)
	resp, err := p.client.Do(ctx, http.MethodPost,
		path, jsonData)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error (status %d): %s",
			resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to read response: %w", err)
	}

	var embResp batchEmbedContentsResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf(
			"failed to parse response: %w", err)
	}

	if len(embResp.Embeddings) != len(texts) {
		return nil, fmt.Errorf(
			"expected %d embeddings, got %d",
			len(texts), len(embResp.Embeddings))
	}

	embeddings := make([][]float32, len(embResp.Embeddings))
	for i, e := range embResp.Embeddings {
		embeddings[i] = e.Values
	}
	return embeddings, nil
}

// Dimensions returns the dimensionality of embeddings.
func (p *EmbeddingProvider) Dimensions() int {
	return p.dimensions
}

// ModelName returns the model name.
func (p *EmbeddingProvider) ModelName() string {
	return p.model
}

var _ llm.EmbeddingProvider = (*EmbeddingProvider)(nil)
