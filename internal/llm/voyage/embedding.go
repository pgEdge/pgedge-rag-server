//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package voyage provides a Voyage AI embedding client.
package voyage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pgEdge/pgedge-rag-server/internal/llm"
)

const (
	defaultBaseURL = "https://api.voyageai.com/v1"
	defaultModel   = "voyage-3"
	defaultTimeout = 60
)

// EmbeddingProvider implements the llm.EmbeddingProvider interface.
type EmbeddingProvider struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	model      string
	dimensions int
}

// NewEmbeddingProvider creates a new Voyage embedding provider.
func NewEmbeddingProvider(apiKey string, opts ...EmbeddingOption) *EmbeddingProvider {
	p := &EmbeddingProvider{
		httpClient: &http.Client{
			Timeout: defaultTimeout * time.Second,
		},
		baseURL:    defaultBaseURL,
		apiKey:     apiKey,
		model:      defaultModel,
		dimensions: 1024, // Default for voyage-3
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// EmbeddingOption configures the embedding provider.
type EmbeddingOption func(*EmbeddingProvider)

// WithModel sets the embedding model.
func WithModel(model string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.model = model
	}
}

// WithDimensions sets the expected embedding dimensions.
func WithDimensions(dims int) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.dimensions = dims
	}
}

// WithBaseURL sets a custom base URL.
func WithBaseURL(url string) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.baseURL = url
	}
}

// WithTimeout sets the HTTP timeout.
func WithTimeout(seconds int) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.httpClient.Timeout = time.Duration(seconds) * time.Second
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.httpClient = client
	}
}

// embeddingRequest is the request format for the embeddings API.
type embeddingRequest struct {
	Model     string   `json:"model"`
	Input     []string `json:"input"`
	InputType string   `json:"input_type,omitempty"`
}

// embeddingResponse is the response format from the embeddings API.
type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// ErrorResponse represents a Voyage API error.
type ErrorResponse struct {
	Detail string `json:"detail"`
}

// Embed generates an embedding for a single text.
func (p *EmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := p.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
func (p *EmbeddingProvider) EmbedBatch(
	ctx context.Context,
	texts []string,
) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := embeddingRequest{
		Model:     p.model,
		Input:     texts,
		InputType: "document",
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		p.baseURL+"/embeddings",
		bytes.NewReader(jsonData),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp ErrorResponse
		if err := json.Unmarshal(body, &errResp); err != nil {
			return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, errResp.Detail)
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Sort embeddings by index to maintain input order
	embeddings := make([][]float32, len(texts))
	for _, d := range embResp.Data {
		if d.Index < len(embeddings) {
			embeddings[d.Index] = d.Embedding
		}
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

// Ensure EmbeddingProvider implements the interface.
var _ llm.EmbeddingProvider = (*EmbeddingProvider)(nil)
