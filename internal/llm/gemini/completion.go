//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package gemini provides a Google Gemini API client.
package gemini

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pgEdge/pgedge-rag-server/internal/llm"
	"github.com/pgEdge/pgedge-rag-server/internal/llm/httpclient"
)

const (
	defaultBaseURL   = "https://generativelanguage.googleapis.com"
	defaultChatModel = "gemini-2.0-flash"
	defaultTimeout   = 60
)

// CompletionProvider implements llm.CompletionProvider.
type CompletionProvider struct {
	client      *httpclient.Client
	apiKey      string
	model       string
	maxTokens   int
	temperature float64
}

// completionConfig holds configuration for building a
// CompletionProvider.
type completionConfig struct {
	model       string
	baseURL     string
	maxTokens   int
	temperature float64
	headers     map[string]string
}

// NewCompletionProvider creates a new Gemini completion provider.
func NewCompletionProvider(
	apiKey string, opts ...CompletionOption,
) *CompletionProvider {
	cfg := &completionConfig{
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

	p := &CompletionProvider{
		client: httpclient.NewClient(
			cfg.baseURL, httpOpts...),
		apiKey:      apiKey,
		model:       defaultChatModel,
		maxTokens:   4096,
		temperature: 0.7,
	}
	if cfg.model != "" {
		p.model = cfg.model
	}
	if cfg.maxTokens > 0 {
		p.maxTokens = cfg.maxTokens
	}
	if cfg.temperature != 0 {
		p.temperature = cfg.temperature
	}
	return p
}

// CompletionOption configures the completion provider.
type CompletionOption func(*completionConfig)

// WithCompletionModel sets the chat model.
func WithCompletionModel(model string) CompletionOption {
	return func(cfg *completionConfig) {
		cfg.model = model
	}
}

// WithCompletionBaseURL sets a custom base URL.
func WithCompletionBaseURL(url string) CompletionOption {
	return func(cfg *completionConfig) {
		cfg.baseURL = url
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

// WithCompletionHeaders sets custom headers.
func WithCompletionHeaders(
	headers map[string]string,
) CompletionOption {
	return func(cfg *completionConfig) {
		cfg.headers = headers
	}
}

// Gemini API types

type part struct {
	Text string `json:"text"`
}

type content struct {
	Parts []part `json:"parts"`
	Role  string `json:"role"`
}

type generationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
}

type generateContentRequest struct {
	Contents          []content         `json:"contents"`
	SystemInstruction *content          `json:"systemInstruction,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
}

type generateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []part `json:"parts"`
			Role  string `json:"role"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// Complete generates a non-streaming completion.
func (p *CompletionProvider) Complete(
	ctx context.Context,
	req llm.CompletionRequest,
) (*llm.CompletionResponse, error) {
	genReq := p.buildRequest(req)

	jsonData, err := json.Marshal(genReq)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to marshal request: %w", err)
	}

	path := fmt.Sprintf(
		"/v1beta/models/%s:generateContent", p.model)
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

	var genResp generateContentResponse
	if err := json.Unmarshal(body, &genResp); err != nil {
		return nil, fmt.Errorf(
			"failed to parse response: %w", err)
	}

	if len(genResp.Candidates) == 0 {
		return nil, fmt.Errorf("no completion returned")
	}

	var text string
	for _, p := range genResp.Candidates[0].Content.Parts {
		text += p.Text
	}

	finishReason := strings.ToLower(
		genResp.Candidates[0].FinishReason)

	return &llm.CompletionResponse{
		Content:      text,
		FinishReason: finishReason,
		Usage: llm.TokenUsage{
			PromptTokens:     genResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: genResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      genResp.UsageMetadata.TotalTokenCount,
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

		genReq := p.buildRequest(req)

		jsonData, err := json.Marshal(genReq)
		if err != nil {
			errChan <- fmt.Errorf(
				"failed to marshal request: %w", err)
			return
		}

		path := fmt.Sprintf(
			"/v1beta/models/%s:streamGenerateContent?alt=sse",
			p.model)
		resp, err := p.client.Do(ctx, http.MethodPost,
			path, jsonData)
		if err != nil {
			errChan <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errChan <- fmt.Errorf(
				"API error (status %d): %s",
				resp.StatusCode, string(body))
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}

			var genResp generateContentResponse
			if err := json.Unmarshal(
				[]byte(data), &genResp); err != nil {
				continue
			}

			if len(genResp.Candidates) == 0 {
				continue
			}

			var text string
			for _, p := range genResp.Candidates[0].Content.Parts {
				text += p.Text
			}

			sc := llm.StreamChunk{
				Content: text,
			}

			finishReason := genResp.Candidates[0].FinishReason
			if finishReason != "" {
				sc.FinishReason = strings.ToLower(finishReason)
				sc.Usage = &llm.TokenUsage{
					PromptTokens:     genResp.UsageMetadata.PromptTokenCount,
					CompletionTokens: genResp.UsageMetadata.CandidatesTokenCount,
					TotalTokens:      genResp.UsageMetadata.TotalTokenCount,
				}
			}

			select {
			case chunkChan <- sc:
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf(
				"stream read error: %w", err)
		}
	}()

	return chunkChan, errChan
}

// buildRequest converts an LLM request to a Gemini API request.
func (p *CompletionProvider) buildRequest(
	req llm.CompletionRequest,
) generateContentRequest {
	genReq := generateContentRequest{
		GenerationConfig: &generationConfig{
			MaxOutputTokens: p.maxTokens,
			Temperature:     p.temperature,
		},
	}

	if req.MaxTokens > 0 {
		genReq.GenerationConfig.MaxOutputTokens = req.MaxTokens
	}
	if req.Temperature >= 0 {
		genReq.GenerationConfig.Temperature = req.Temperature
	}

	// Build system instruction
	var systemParts []string
	if req.SystemPrompt != "" {
		systemParts = append(systemParts, req.SystemPrompt)
	}
	if len(req.Context) > 0 {
		systemParts = append(systemParts,
			llm.FormatContext(req.Context))
	}
	if len(systemParts) > 0 {
		genReq.SystemInstruction = &content{
			Parts: []part{
				{Text: strings.Join(systemParts, "\n\n")},
			},
		}
	}

	// Build contents (conversation messages)
	for _, msg := range req.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}
		genReq.Contents = append(genReq.Contents, content{
			Parts: []part{{Text: msg.Content}},
			Role:  role,
		})
	}

	return genReq
}

// ModelName returns the model name.
func (p *CompletionProvider) ModelName() string {
	return p.model
}

var _ llm.CompletionProvider = (*CompletionProvider)(nil)
