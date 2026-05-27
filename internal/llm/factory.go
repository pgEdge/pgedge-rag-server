//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package llm

import (
	"fmt"
	"strings"

	llmlib "github.com/pgEdge/pgedge-go-llm-lib/llm"
	_ "github.com/pgEdge/pgedge-go-llm-lib/llm/all" // register all providers

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

// Provider name constants. Matches the strings accepted in YAML
// configuration (case-insensitive at the boundary).
const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
	ProviderGemini    = "gemini"
	ProviderVoyage    = "voyage"
	ProviderOllama    = "ollama"
)

// NewEmbeddingClient builds an llm.Client for embeddings. The factory
// validates that the provider supports embeddings and that the
// necessary API key (or base URL substitute) is present, then delegates
// to llmlib.NewClient.
func NewEmbeddingClient(
	provider, model, baseURL string,
	headers map[string]string,
	keys *config.LoadedKeys,
) (llmlib.Client, error) {
	p := strings.ToLower(provider)

	switch p {
	case ProviderAnthropic:
		return nil, fmt.Errorf("Anthropic does not provide an embedding API")
	case ProviderOpenAI:
		if keys.OpenAI == "" && baseURL == "" {
			return nil, fmt.Errorf("OpenAI API key or base URL required")
		}
		return llmlib.NewClient(p, llmlib.Options{
			APIKey:        keys.OpenAI,
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		})
	case ProviderVoyage:
		if keys.Voyage == "" {
			return nil, fmt.Errorf("Voyage API key not configured")
		}
		return llmlib.NewClient(p, llmlib.Options{
			APIKey:        keys.Voyage,
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		})
	case ProviderGemini:
		if keys.Gemini == "" {
			return nil, fmt.Errorf("Gemini API key not configured")
		}
		return llmlib.NewClient(p, llmlib.Options{
			APIKey:        keys.Gemini,
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		})
	case ProviderOllama:
		return llmlib.NewClient(p, llmlib.Options{
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		})
	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", provider)
	}
}
