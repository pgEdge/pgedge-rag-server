//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package ollama

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/pgEdge/pgedge-rag-server/internal/llm"
)

// CompletionProvider implements the llm.CompletionProvider interface.
type CompletionProvider struct {
	client      *Client
	model       string
	temperature float64
}

// NewCompletionProvider creates a new Ollama completion provider.
func NewCompletionProvider(opts ...CompletionOption) *CompletionProvider {
	p := &CompletionProvider{
		client:      NewClient(),
		model:       defaultChatModel,
		temperature: 0.7,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// CompletionOption configures the completion provider.
type CompletionOption func(*CompletionProvider)

// WithCompletionModel sets the chat model.
func WithCompletionModel(model string) CompletionOption {
	return func(p *CompletionProvider) {
		p.model = model
	}
}

// WithTemperature sets the default temperature.
func WithTemperature(temp float64) CompletionOption {
	return func(p *CompletionProvider) {
		p.temperature = temp
	}
}

// WithCompletionClient sets a custom client.
func WithCompletionClient(client *Client) CompletionOption {
	return func(p *CompletionProvider) {
		p.client = client
	}
}

// chatMessage represents a message in Ollama's chat format.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the request format for the chat API.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Options  *chatOptions  `json:"options,omitempty"`
}

// chatOptions contains generation options.
type chatOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
}

// chatResponse is the response format from the chat API.
type chatResponse struct {
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done               bool `json:"done"`
	PromptEvalCount    int  `json:"prompt_eval_count"`
	EvalCount          int  `json:"eval_count"`
	TotalDuration      int  `json:"total_duration"`
	LoadDuration       int  `json:"load_duration"`
	PromptEvalDuration int  `json:"prompt_eval_duration"`
	EvalDuration       int  `json:"eval_duration"`
}

// Complete generates a non-streaming completion.
func (p *CompletionProvider) Complete(
	ctx context.Context,
	req llm.CompletionRequest,
) (*llm.CompletionResponse, error) {
	messages := p.buildMessages(req)

	temperature := p.temperature
	if req.Temperature >= 0 {
		temperature = req.Temperature
	}

	chatReq := chatRequest{
		Model:    p.model,
		Messages: messages,
		Stream:   false,
		Options: &chatOptions{
			Temperature: temperature,
			NumPredict:  req.MaxTokens,
		},
	}

	resp, err := p.client.request(ctx, http.MethodPost, "/api/chat", chatReq)
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

	var chatResp chatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	finishReason := "stop"
	if !chatResp.Done {
		finishReason = "length"
	}

	return &llm.CompletionResponse{
		Content:      chatResp.Message.Content,
		FinishReason: finishReason,
		Usage: llm.TokenUsage{
			PromptTokens:     chatResp.PromptEvalCount,
			CompletionTokens: chatResp.EvalCount,
			TotalTokens:      chatResp.PromptEvalCount + chatResp.EvalCount,
		},
	}, nil
}

// CompleteStream generates a streaming completion.
func (p *CompletionProvider) CompleteStream(
	ctx context.Context,
	req llm.CompletionRequest,
) (<-chan llm.StreamChunk, <-chan error) {
	chunkChan := make(chan llm.StreamChunk)
	errChan := make(chan error, 1)

	go func() {
		defer close(chunkChan)
		defer close(errChan)

		messages := p.buildMessages(req)

		temperature := p.temperature
		if req.Temperature >= 0 {
			temperature = req.Temperature
		}

		chatReq := chatRequest{
			Model:    p.model,
			Messages: messages,
			Stream:   true,
			Options: &chatOptions{
				Temperature: temperature,
				NumPredict:  req.MaxTokens,
			},
		}

		resp, err := p.client.request(ctx, http.MethodPost, "/api/chat", chatReq)
		if err != nil {
			errChan <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			errChan <- parseError(resp)
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}

			var chunk chatResponse
			if err := json.Unmarshal([]byte(line), &chunk); err != nil {
				continue
			}

			streamChunk := llm.StreamChunk{
				Content: chunk.Message.Content,
			}

			if chunk.Done {
				streamChunk.FinishReason = "stop"
				streamChunk.Usage = &llm.TokenUsage{
					PromptTokens:     chunk.PromptEvalCount,
					CompletionTokens: chunk.EvalCount,
					TotalTokens:      chunk.PromptEvalCount + chunk.EvalCount,
				}
			}

			select {
			case chunkChan <- streamChunk:
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			}

			if chunk.Done {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("stream read error: %w", err)
		}
	}()

	return chunkChan, errChan
}

// buildMessages converts the request into Ollama chat messages.
func (p *CompletionProvider) buildMessages(req llm.CompletionRequest) []chatMessage {
	// Calculate capacity: up to 2 system messages + all conversation messages
	capacity := len(req.Messages)
	if req.SystemPrompt != "" {
		capacity++
	}
	if len(req.Context) > 0 {
		capacity++
	}
	messages := make([]chatMessage, 0, capacity)

	// Add system prompt if provided
	if req.SystemPrompt != "" {
		messages = append(messages, chatMessage{
			Role:    "system",
			Content: req.SystemPrompt,
		})
	}

	// Add context documents as a system message if provided
	if len(req.Context) > 0 {
		contextContent := llm.FormatContext(req.Context)
		messages = append(messages, chatMessage{
			Role:    "system",
			Content: contextContent,
		})
	}

	// Add conversation messages
	for _, msg := range req.Messages {
		messages = append(messages, chatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	return messages
}

// ModelName returns the model name.
func (p *CompletionProvider) ModelName() string {
	return p.model
}

// Ensure CompletionProvider implements the interface.
var _ llm.CompletionProvider = (*CompletionProvider)(nil)
