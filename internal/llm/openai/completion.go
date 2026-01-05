//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pgEdge/pgedge-rag-server/internal/llm"
)

// CompletionProvider implements the llm.CompletionProvider interface.
type CompletionProvider struct {
	client      *Client
	model       string
	maxTokens   int
	temperature float64
}

// NewCompletionProvider creates a new OpenAI completion provider.
func NewCompletionProvider(apiKey string, opts ...CompletionOption) *CompletionProvider {
	p := &CompletionProvider{
		client:      NewClient(apiKey),
		model:       defaultChatModel,
		maxTokens:   4096,
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

// WithMaxTokens sets the default max tokens.
func WithMaxTokens(tokens int) CompletionOption {
	return func(p *CompletionProvider) {
		p.maxTokens = tokens
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

// chatMessage represents a message in the chat format.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the request format for the chat completions API.
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

// chatResponse is the response format from the chat completions API.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// streamChunk represents a streaming response chunk.
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// Complete generates a non-streaming completion.
func (p *CompletionProvider) Complete(
	ctx context.Context,
	req llm.CompletionRequest,
) (*llm.CompletionResponse, error) {
	messages := p.buildMessages(req)

	maxTokens := p.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	temperature := p.temperature
	if req.Temperature >= 0 {
		temperature = req.Temperature
	}

	chatReq := chatRequest{
		Model:       p.model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Stream:      false,
	}

	resp, err := p.client.request(ctx, http.MethodPost, "/chat/completions", chatReq)
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

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no completion returned")
	}

	return &llm.CompletionResponse{
		Content:      chatResp.Choices[0].Message.Content,
		FinishReason: chatResp.Choices[0].FinishReason,
		Usage: llm.TokenUsage{
			PromptTokens:     chatResp.Usage.PromptTokens,
			CompletionTokens: chatResp.Usage.CompletionTokens,
			TotalTokens:      chatResp.Usage.TotalTokens,
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

		maxTokens := p.maxTokens
		if req.MaxTokens > 0 {
			maxTokens = req.MaxTokens
		}

		temperature := p.temperature
		if req.Temperature >= 0 {
			temperature = req.Temperature
		}

		chatReq := chatRequest{
			Model:       p.model,
			Messages:    messages,
			MaxTokens:   maxTokens,
			Temperature: temperature,
			Stream:      true,
		}

		resp, err := p.client.request(ctx, http.MethodPost, "/chat/completions", chatReq)
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
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // Skip malformed chunks
			}

			if len(chunk.Choices) == 0 {
				continue
			}

			streamChunk := llm.StreamChunk{
				Content:      chunk.Choices[0].Delta.Content,
				FinishReason: chunk.Choices[0].FinishReason,
			}

			if chunk.Usage != nil {
				streamChunk.Usage = &llm.TokenUsage{
					PromptTokens:     chunk.Usage.PromptTokens,
					CompletionTokens: chunk.Usage.CompletionTokens,
					TotalTokens:      chunk.Usage.TotalTokens,
				}
			}

			select {
			case chunkChan <- streamChunk:
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("stream read error: %w", err)
		}
	}()

	return chunkChan, errChan
}

// buildMessages converts the request into OpenAI chat messages.
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
