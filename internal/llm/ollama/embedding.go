//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/pgEdge/pgedge-rag-server/internal/llm"
)

// EmbeddingProvider implements the llm.EmbeddingProvider interface.
type EmbeddingProvider struct {
	client     *Client
	model      string
	dimensions int
}

// NewEmbeddingProvider creates a new Ollama embedding provider.
func NewEmbeddingProvider(opts ...EmbeddingOption) *EmbeddingProvider {
	p := &EmbeddingProvider{
		client:     NewClient(),
		model:      defaultEmbeddingModel,
		dimensions: 768, // Default for nomic-embed-text
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// EmbeddingOption configures the embedding provider.
type EmbeddingOption func(*EmbeddingProvider)

// WithEmbeddingModel sets the embedding model.
func WithEmbeddingModel(model string) EmbeddingOption {
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

// WithEmbeddingClient sets a custom client.
func WithEmbeddingClient(client *Client) EmbeddingOption {
	return func(p *EmbeddingProvider) {
		p.client = client
	}
}

// embeddingRequest is the request format for the embeddings API.
type embeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// embeddingResponse is the response format from the embeddings API.
type embeddingResponse struct {
	Embedding []float64 `json:"embedding"` // Ollama returns float64
}

// Embed generates an embedding for a single text.
func (p *EmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := embeddingRequest{
		Model:  p.model,
		Prompt: text,
	}

	resp, err := p.client.request(ctx, http.MethodPost, "/api/embeddings", reqBody)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(body, &embResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert float64 to float32
	embedding := make([]float32, len(embResp.Embedding))
	for i, v := range embResp.Embedding {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

// EmbedBatch generates embeddings for multiple texts.
// Note: Ollama doesn't support batch embeddings, so we call Embed for each text.
func (p *EmbeddingProvider) EmbedBatch(
	ctx context.Context,
	texts []string,
) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	embeddings := make([][]float32, len(texts))
	for i, text := range texts {
		emb, err := p.Embed(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("failed to embed text %d: %w", i, err)
		}
		embeddings[i] = emb
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
