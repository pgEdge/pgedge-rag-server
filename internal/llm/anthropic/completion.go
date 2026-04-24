//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package anthropic

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

// completionConfig holds configuration for building a CompletionProvider.
type completionConfig struct {
	model       string
	baseURL     string
	maxTokens   int
	temperature float64
	headers     map[string]string
}

// CompletionOption configures the completion provider.
type CompletionOption func(*completionConfig)

// WithCompletionModel sets the model.
func WithCompletionModel(model string) CompletionOption {
	return func(cfg *completionConfig) {
		cfg.model = model
	}
}

// WithMaxTokens sets the default max tokens.
func WithMaxTokens(tokens int) CompletionOption {
	return func(cfg *completionConfig) {
		cfg.maxTokens = tokens
	}
}

// WithTemperature sets the default temperature.
func WithTemperature(temp float64) CompletionOption {
	return func(cfg *completionConfig) {
		cfg.temperature = temp
	}
}

// WithCompletionBaseURL sets a custom base URL for the completion provider.
func WithCompletionBaseURL(url string) CompletionOption {
	return func(cfg *completionConfig) {
		cfg.baseURL = url
	}
}

// WithCompletionHeaders sets custom headers for the completion provider.
func WithCompletionHeaders(headers map[string]string) CompletionOption {
	return func(cfg *completionConfig) {
		cfg.headers = headers
	}
}

// NewCompletionProvider creates a new Anthropic completion provider.
func NewCompletionProvider(apiKey string, opts ...CompletionOption) *CompletionProvider {
	cfg := &completionConfig{
		model:       defaultModel,
		maxTokens:   4096,
		temperature: 0.7,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Build client options from the completion config
	var clientOpts []ClientOption
	if cfg.baseURL != "" {
		clientOpts = append(clientOpts, WithBaseURL(cfg.baseURL))
	}
	if len(cfg.headers) > 0 {
		clientOpts = append(clientOpts, WithClientHeaders(cfg.headers))
	}

	return &CompletionProvider{
		client:      NewClient(apiKey, clientOpts...),
		model:       cfg.model,
		maxTokens:   cfg.maxTokens,
		temperature: cfg.temperature,
	}
}

// message represents a message in Anthropic's format.
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// messagesRequest is the request format for the messages API.
type messagesRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	System      string    `json:"system,omitempty"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// messagesResponse is the response format from the messages API.
type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// streamEvent represents a streaming event.
type streamEvent struct {
	Type  string `json:"type"`
	Delta *struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta,omitempty"`
	Message *struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message,omitempty"`
	Usage *struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage,omitempty"`
}

// Complete generates a non-streaming completion.
func (p *CompletionProvider) Complete(
	ctx context.Context,
	req llm.CompletionRequest,
) (*llm.CompletionResponse, error) {
	messages, system := p.buildMessages(req)

	maxTokens := p.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	temperature := p.temperature
	if req.Temperature >= 0 {
		temperature = req.Temperature
	}

	msgReq := messagesRequest{
		Model:       p.model,
		MaxTokens:   maxTokens,
		System:      system,
		Messages:    messages,
		Temperature: temperature,
		Stream:      false,
	}

	jsonData, err := json.Marshal(msgReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := p.client.http.Do(
		ctx, http.MethodPost, "/messages", jsonData)
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

	var msgResp messagesResponse
	if err := json.Unmarshal(body, &msgResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract text content
	var content string
	for _, c := range msgResp.Content {
		if c.Type == "text" {
			content += c.Text
		}
	}

	return &llm.CompletionResponse{
		Content:      content,
		FinishReason: msgResp.StopReason,
		Usage: llm.TokenUsage{
			PromptTokens:     msgResp.Usage.InputTokens,
			CompletionTokens: msgResp.Usage.OutputTokens,
			TotalTokens:      msgResp.Usage.InputTokens + msgResp.Usage.OutputTokens,
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

		messages, system := p.buildMessages(req)

		maxTokens := p.maxTokens
		if req.MaxTokens > 0 {
			maxTokens = req.MaxTokens
		}

		temperature := p.temperature
		if req.Temperature >= 0 {
			temperature = req.Temperature
		}

		msgReq := messagesRequest{
			Model:       p.model,
			MaxTokens:   maxTokens,
			System:      system,
			Messages:    messages,
			Temperature: temperature,
			Stream:      true,
		}

		jsonData, err := json.Marshal(msgReq)
		if err != nil {
			errChan <- fmt.Errorf("failed to marshal request: %w", err)
			return
		}

		resp, err := p.client.http.Do(
			ctx, http.MethodPost, "/messages", jsonData)
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
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		var inputTokens, outputTokens int

		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}

			var event streamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				errChan <- fmt.Errorf(
					"stream JSON decode error: %w", err)
				return
			}

			switch event.Type {
			case "message_start":
				if event.Message != nil {
					inputTokens = event.Message.Usage.InputTokens
				}
			case "content_block_delta":
				if event.Delta != nil && event.Delta.Type == "text_delta" {
					select {
					case chunkChan <- llm.StreamChunk{Content: event.Delta.Text}:
					case <-ctx.Done():
						errChan <- ctx.Err()
						return
					}
				}
			case "message_delta":
				if event.Delta != nil {
					if event.Usage != nil {
						outputTokens = event.Usage.OutputTokens
					}
					if event.Delta.StopReason != "" {
						select {
						case chunkChan <- llm.StreamChunk{
							FinishReason: event.Delta.StopReason,
							Usage: &llm.TokenUsage{
								PromptTokens:     inputTokens,
								CompletionTokens: outputTokens,
								TotalTokens:      inputTokens + outputTokens,
							},
						}:
						case <-ctx.Done():
							errChan <- ctx.Err()
							return
						}
					}
				}
			case "message_stop":
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("stream read error: %w", err)
		}
	}()

	return chunkChan, errChan
}

// buildMessages converts the request into Anthropic messages format.
// Returns messages and system prompt separately (Anthropic's format).
func (p *CompletionProvider) buildMessages(req llm.CompletionRequest) ([]message, string) {
	// Pre-allocate messages slice (may be slightly over-allocated if
	// there are system messages)
	messages := make([]message, 0, len(req.Messages))
	var systemParts []string

	// Build system prompt
	if req.SystemPrompt != "" {
		systemParts = append(systemParts, req.SystemPrompt)
	}

	// Add context documents to system prompt
	if len(req.Context) > 0 {
		systemParts = append(systemParts, llm.FormatContext(req.Context))
	}

	system := strings.Join(systemParts, "\n\n")

	// Add conversation messages
	for _, msg := range req.Messages {
		// Anthropic only accepts "user" and "assistant" roles
		role := msg.Role
		if role == "system" {
			// Prepend system messages to the system prompt
			system = msg.Content + "\n\n" + system
			continue
		}
		messages = append(messages, message{
			Role:    role,
			Content: msg.Content,
		})
	}

	return messages, system
}

// ModelName returns the model name.
func (p *CompletionProvider) ModelName() string {
	return p.model
}

// Ensure CompletionProvider implements the interface.
var _ llm.CompletionProvider = (*CompletionProvider)(nil)
