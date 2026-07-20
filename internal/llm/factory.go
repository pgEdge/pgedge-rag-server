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
	"time"

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

// clientOptions collects the optional, provider-independent settings a
// caller can apply to a client. It exists so the timeout knobs can be
// threaded through the factory without expanding every call site.
type clientOptions struct {
	requestTimeout    time.Duration
	perAttemptTimeout time.Duration
}

// ClientOption customises client construction.
type ClientOption func(*clientOptions)

// WithRequestTimeout sets the overall request timeout (zero leaves the
// library default in place).
func WithRequestTimeout(d time.Duration) ClientOption {
	return func(o *clientOptions) { o.requestTimeout = d }
}

// WithPerAttemptTimeout sets the per-attempt timeout (zero disables it).
func WithPerAttemptTimeout(d time.Duration) ClientOption {
	return func(o *clientOptions) { o.perAttemptTimeout = d }
}

// withOptions stamps the resolved ClientOptions onto a base
// llmlib.Options so every provider branch shares identical timeout
// wiring.
func withOptions(base llmlib.Options, opts []ClientOption) llmlib.Options {
	var co clientOptions
	for _, fn := range opts {
		fn(&co)
	}
	base.RequestTimeout = co.requestTimeout
	base.PerAttemptTimeout = co.perAttemptTimeout
	return base
}

// NewEmbeddingClient builds an llm.Client for embeddings. The factory
// validates that the provider supports embeddings and that the
// necessary API key (or base URL substitute) is present, then delegates
// to llmlib.NewClient.
func NewEmbeddingClient(
	provider, model, baseURL string,
	headers map[string]string,
	keys *config.LoadedKeys,
	opts ...ClientOption,
) (llmlib.Client, error) {
	if keys == nil {
		keys = &config.LoadedKeys{}
	}
	p := strings.ToLower(provider)

	switch p {
	case ProviderAnthropic:
		return nil, fmt.Errorf("Anthropic does not provide an embedding API")
	case ProviderOpenAI:
		if keys.OpenAI == "" && baseURL == "" {
			return nil, fmt.Errorf("OpenAI API key or base URL required")
		}
		return llmlib.NewClient(p, withOptions(llmlib.Options{
			APIKey:        keys.OpenAI,
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		}, opts))
	case ProviderVoyage:
		if keys.Voyage == "" {
			return nil, fmt.Errorf("Voyage API key not configured")
		}
		return llmlib.NewClient(p, withOptions(llmlib.Options{
			APIKey:        keys.Voyage,
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		}, opts))
	case ProviderGemini:
		if keys.Gemini == "" {
			return nil, fmt.Errorf("Gemini API key not configured")
		}
		return llmlib.NewClient(p, withOptions(llmlib.Options{
			APIKey:        keys.Gemini,
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		}, opts))
	case ProviderOllama:
		return llmlib.NewClient(p, withOptions(llmlib.Options{
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		}, opts))
	default:
		return nil, fmt.Errorf("unknown embedding provider: %s", provider)
	}
}

// NewCompletionClient builds an llm.Client for chat completion. The
// factory validates that the provider supports completion (Voyage is
// embeddings-only) and that the necessary API key is present.
func NewCompletionClient(
	provider, model, baseURL string,
	headers map[string]string,
	keys *config.LoadedKeys,
	opts ...ClientOption,
) (llmlib.Client, error) {
	if keys == nil {
		keys = &config.LoadedKeys{}
	}
	p := strings.ToLower(provider)

	switch p {
	case ProviderVoyage:
		return nil, fmt.Errorf("Voyage does not provide a completion API")
	case ProviderOpenAI:
		if keys.OpenAI == "" && baseURL == "" {
			return nil, fmt.Errorf("OpenAI API key or base URL required")
		}
		return llmlib.NewClient(p, withOptions(llmlib.Options{
			APIKey:        keys.OpenAI,
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		}, opts))
	case ProviderAnthropic:
		if keys.Anthropic == "" {
			return nil, fmt.Errorf("Anthropic API key not configured")
		}
		return llmlib.NewClient(p, withOptions(llmlib.Options{
			APIKey:        keys.Anthropic,
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		}, opts))
	case ProviderGemini:
		if keys.Gemini == "" {
			return nil, fmt.Errorf("Gemini API key not configured")
		}
		return llmlib.NewClient(p, withOptions(llmlib.Options{
			APIKey:        keys.Gemini,
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		}, opts))
	case ProviderOllama:
		return llmlib.NewClient(p, withOptions(llmlib.Options{
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		}, opts))
	default:
		return nil, fmt.Errorf("unknown completion provider: %s", provider)
	}
}

// NewRerankClient builds an llm.Client for reranking. The factory
// rejects every provider except Voyage: it is currently the only
// provider in pgedge-go-llm-lib whose Rerank implementation is not a
// stub, so rejecting the others at construction time (rather than
// deferring to their runtime ErrNotSupported) matches how
// NewEmbeddingClient/NewCompletionClient already reject providers that
// don't support the capability being requested.
func NewRerankClient(
	provider, model, baseURL string,
	headers map[string]string,
	keys *config.LoadedKeys,
	opts ...ClientOption,
) (llmlib.Client, error) {
	if keys == nil {
		keys = &config.LoadedKeys{}
	}
	p := strings.ToLower(provider)

	switch p {
	case ProviderVoyage:
		if keys.Voyage == "" {
			return nil, fmt.Errorf("Voyage API key not configured")
		}
		return llmlib.NewClient(p, withOptions(llmlib.Options{
			APIKey:        keys.Voyage,
			Model:         model,
			BaseURL:       baseURL,
			CustomHeaders: headers,
		}, opts))
	default:
		return nil, fmt.Errorf("provider %s does not support reranking", provider)
	}
}
