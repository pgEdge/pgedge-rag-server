//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package factory provides functions to create LLM providers from configuration.
package factory

import (
	"fmt"
	"strings"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/llm"
	"github.com/pgEdge/pgedge-rag-server/internal/llm/anthropic"
	"github.com/pgEdge/pgedge-rag-server/internal/llm/gemini"
	"github.com/pgEdge/pgedge-rag-server/internal/llm/ollama"
	"github.com/pgEdge/pgedge-rag-server/internal/llm/openai"
	"github.com/pgEdge/pgedge-rag-server/internal/llm/voyage"
)

// Provider constants for matching configuration values.
const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
	ProviderGemini    = "gemini"
	ProviderVoyage    = "voyage"
	ProviderOllama    = "ollama"
)

// NewEmbeddingProvider creates an embedding provider based on configuration.
func NewEmbeddingProvider(
	providerType string,
	model string,
	baseURL string,
	headers map[string]string,
	apiKeys *config.LoadedKeys,
) (llm.EmbeddingProvider, error) {
	provider := strings.ToLower(providerType)

	switch provider {
	case ProviderOpenAI:
		if apiKeys.OpenAI == "" && baseURL == "" {
			return nil, fmt.Errorf("OpenAI API key or base URL required")
		}
		opts := []openai.EmbeddingOption{}
		if model != "" {
			opts = append(opts, openai.WithEmbeddingModel(model))
		}
		if baseURL != "" {
			opts = append(opts, openai.WithEmbeddingBaseURL(baseURL))
		}
		if len(headers) > 0 {
			opts = append(opts, openai.WithEmbeddingHeaders(headers))
		}
		return openai.NewEmbeddingProvider(apiKeys.OpenAI, opts...), nil

	case ProviderVoyage:
		if apiKeys.Voyage == "" {
			return nil, fmt.Errorf("Voyage API key not configured")
		}
		opts := []voyage.EmbeddingOption{}
		if model != "" {
			opts = append(opts, voyage.WithModel(model))
		}
		if baseURL != "" {
			opts = append(opts, voyage.WithEmbeddingBaseURL(baseURL))
		}
		return voyage.NewEmbeddingProvider(apiKeys.Voyage, opts...), nil

	case ProviderOllama:
		opts := []ollama.EmbeddingOption{}
		if model != "" {
			opts = append(opts, ollama.WithEmbeddingModel(model))
		}
		if baseURL != "" {
			opts = append(opts, ollama.WithEmbeddingBaseURL(baseURL))
		}
		return ollama.NewEmbeddingProvider(opts...), nil

	case ProviderGemini:
		if apiKeys.Gemini == "" {
			return nil, fmt.Errorf("Gemini API key not configured")
		}
		opts := []gemini.EmbeddingOption{}
		if model != "" {
			opts = append(opts, gemini.WithEmbeddingModel(model))
		}
		if baseURL != "" {
			opts = append(opts, gemini.WithEmbeddingBaseURL(baseURL))
		}
		if len(headers) > 0 {
			opts = append(opts, gemini.WithEmbeddingHeaders(headers))
		}
		return gemini.NewEmbeddingProvider(apiKeys.Gemini, opts...), nil

	case ProviderAnthropic:
		return nil, fmt.Errorf("Anthropic does not provide an embedding API")

	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", providerType)
	}
}

// NewCompletionProvider creates a completion provider based on configuration.
func NewCompletionProvider(
	providerType string,
	model string,
	baseURL string,
	headers map[string]string,
	apiKeys *config.LoadedKeys,
) (llm.CompletionProvider, error) {
	provider := strings.ToLower(providerType)

	switch provider {
	case ProviderOpenAI:
		if apiKeys.OpenAI == "" && baseURL == "" {
			return nil, fmt.Errorf("OpenAI API key or base URL required")
		}
		opts := []openai.CompletionOption{}
		if model != "" {
			opts = append(opts, openai.WithCompletionModel(model))
		}
		if baseURL != "" {
			opts = append(opts, openai.WithCompletionBaseURL(baseURL))
		}
		if len(headers) > 0 {
			opts = append(opts, openai.WithCompletionHeaders(headers))
		}
		return openai.NewCompletionProvider(apiKeys.OpenAI, opts...), nil

	case ProviderAnthropic:
		if apiKeys.Anthropic == "" {
			return nil, fmt.Errorf("Anthropic API key not configured")
		}
		opts := []anthropic.CompletionOption{}
		if model != "" {
			opts = append(opts, anthropic.WithCompletionModel(model))
		}
		if baseURL != "" {
			opts = append(opts, anthropic.WithCompletionBaseURL(baseURL))
		}
		return anthropic.NewCompletionProvider(apiKeys.Anthropic, opts...), nil

	case ProviderOllama:
		opts := []ollama.CompletionOption{}
		if model != "" {
			opts = append(opts, ollama.WithCompletionModel(model))
		}
		if baseURL != "" {
			opts = append(opts, ollama.WithCompletionBaseURL(baseURL))
		}
		return ollama.NewCompletionProvider(opts...), nil

	case ProviderGemini:
		if apiKeys.Gemini == "" {
			return nil, fmt.Errorf("Gemini API key not configured")
		}
		opts := []gemini.CompletionOption{}
		if model != "" {
			opts = append(opts, gemini.WithCompletionModel(model))
		}
		if baseURL != "" {
			opts = append(opts, gemini.WithCompletionBaseURL(baseURL))
		}
		if len(headers) > 0 {
			opts = append(opts, gemini.WithCompletionHeaders(headers))
		}
		return gemini.NewCompletionProvider(apiKeys.Gemini, opts...), nil

	case ProviderVoyage:
		return nil, fmt.Errorf("Voyage does not provide a completion API")

	default:
		return nil, fmt.Errorf("unknown completion provider: %s", providerType)
	}
}
