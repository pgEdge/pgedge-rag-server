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
	"github.com/pgEdge/pgedge-rag-server/internal/llm/ollama"
	"github.com/pgEdge/pgedge-rag-server/internal/llm/openai"
	"github.com/pgEdge/pgedge-rag-server/internal/llm/voyage"
)

// Provider constants for matching configuration values.
const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
	ProviderVoyage    = "voyage"
	ProviderOllama    = "ollama"
)

// NewEmbeddingProvider creates an embedding provider based on configuration.
func NewEmbeddingProvider(
	providerType string,
	model string,
	apiKeys *config.LoadedKeys,
) (llm.EmbeddingProvider, error) {
	provider := strings.ToLower(providerType)

	switch provider {
	case ProviderOpenAI:
		if apiKeys.OpenAI == "" {
			return nil, fmt.Errorf("OpenAI API key not configured")
		}
		opts := []openai.EmbeddingOption{}
		if model != "" {
			opts = append(opts, openai.WithEmbeddingModel(model))
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
		return voyage.NewEmbeddingProvider(apiKeys.Voyage, opts...), nil

	case ProviderOllama:
		opts := []ollama.EmbeddingOption{}
		if model != "" {
			opts = append(opts, ollama.WithEmbeddingModel(model))
		}
		return ollama.NewEmbeddingProvider(opts...), nil

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
	apiKeys *config.LoadedKeys,
) (llm.CompletionProvider, error) {
	provider := strings.ToLower(providerType)

	switch provider {
	case ProviderOpenAI:
		if apiKeys.OpenAI == "" {
			return nil, fmt.Errorf("OpenAI API key not configured")
		}
		opts := []openai.CompletionOption{}
		if model != "" {
			opts = append(opts, openai.WithCompletionModel(model))
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
		return anthropic.NewCompletionProvider(apiKeys.Anthropic, opts...), nil

	case ProviderOllama:
		opts := []ollama.CompletionOption{}
		if model != "" {
			opts = append(opts, ollama.WithCompletionModel(model))
		}
		return ollama.NewCompletionProvider(opts...), nil

	case ProviderVoyage:
		return nil, fmt.Errorf("Voyage does not provide a completion API")

	default:
		return nil, fmt.Errorf("unknown completion provider: %s", providerType)
	}
}
