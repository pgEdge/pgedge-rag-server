//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package config handles configuration loading and validation for the
// pgEdge RAG Server.
package config

import "fmt"

// Config is the root configuration structure for the server.
type Config struct {
	Server    ServerConfig  `yaml:"server"`
	APIKeys   APIKeysConfig `yaml:"api_keys"`
	Defaults  Defaults      `yaml:"defaults"`
	Pipelines []Pipeline    `yaml:"pipelines"`
}

// APIKeysConfig contains paths to files containing API keys for LLM providers.
// If not specified, keys are loaded from environment variables or default
// file locations (~/.anthropic-api-key, ~/.openai-api-key, ~/.voyage-api-key,
// ~/.gemini-api-key).
type APIKeysConfig struct {
	Anthropic string `yaml:"anthropic"` // Path to file containing Anthropic API key
	OpenAI    string `yaml:"openai"`    // Path to file containing OpenAI API key
	Voyage    string `yaml:"voyage"`    // Path to file containing Voyage API key
	Gemini    string `yaml:"gemini"`    // Path to file containing Gemini API key
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	ListenAddress string     `yaml:"listen_address"`
	Port          int        `yaml:"port"`
	TLS           TLSConfig  `yaml:"tls"`
	CORS          CORSConfig `yaml:"cors"`
}

// CORSConfig contains CORS (Cross-Origin Resource Sharing) settings.
type CORSConfig struct {
	Enabled        bool     `yaml:"enabled"`
	AllowedOrigins []string `yaml:"allowed_origins"` // Origins to allow, or ["*"] for all
}

// TLSConfig contains TLS/HTTPS settings.
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Defaults contains default values that can be overridden per-pipeline.
type Defaults struct {
	TokenBudget  int               `yaml:"token_budget"`
	TopN         int               `yaml:"top_n"`
	EmbeddingLLM LLMConfig         `yaml:"embedding_llm"` // Default embedding provider
	RAGLLM       LLMConfig         `yaml:"rag_llm"`       // Default completion provider
	APIKeys      APIKeysConfig     `yaml:"api_keys"`      // Default API key paths
	LLMHeaders   map[string]string `yaml:"llm_headers"`   // Default headers for LLM calls
}

// Pipeline defines a single RAG pipeline configuration.
type Pipeline struct {
	Name         string            `yaml:"name"`
	Description  string            `yaml:"description"`
	Database     DatabaseConfig    `yaml:"database"`
	Tables       []TableSource     `yaml:"tables"`
	EmbeddingLLM LLMConfig         `yaml:"embedding_llm"`
	RAGLLM       LLMConfig         `yaml:"rag_llm"`
	APIKeys      APIKeysConfig     `yaml:"api_keys"` // Pipeline-specific API key paths
	TokenBudget  int               `yaml:"token_budget"`
	TopN         int               `yaml:"top_n"`
	SystemPrompt string            `yaml:"system_prompt"` // Custom system prompt for LLM
	Search       SearchConfig      `yaml:"search"`        // Search behavior settings
	LLMHeaders   map[string]string `yaml:"llm_headers"`   // Pipeline-level headers for LLM calls
}

// HostEntry represents a single host in a multi-host database configuration.
type HostEntry struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// DatabaseConfig contains PostgreSQL connection settings.
type DatabaseConfig struct {
	// Single-host connection fields
	Host string `yaml:"host"`
	Port int    `yaml:"port"`

	// Multi-host connection fields (for HA deployments)
	Hosts              []HostEntry `yaml:"hosts"`
	TargetSessionAttrs string      `yaml:"target_session_attrs"`

	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	SSLMode  string `yaml:"ssl_mode"`

	// Certificate-based authentication
	SSLCert   string `yaml:"ssl_cert"`
	SSLKey    string `yaml:"ssl_key"`
	SSLRootCA string `yaml:"ssl_root_ca"`
}

// TableSource defines a table with text and vector columns for hybrid search.
type TableSource struct {
	Table        string        `yaml:"table"`
	TextColumn   string        `yaml:"text_column"`
	VectorColumn string        `yaml:"vector_column"`
	IDColumn     string        `yaml:"id_column"` // Optional ID column (required for views)
	Filter       *ConfigFilter `yaml:"filter"`    // Optional filter (raw SQL or structured)
}

// SearchConfig contains settings for search behavior.
type SearchConfig struct {
	HybridEnabled *bool    `yaml:"hybrid_enabled"` // Enable hybrid search (default: true)
	VectorWeight  *float64 `yaml:"vector_weight"`  // Weight for vector vs BM25 (default: 0.5)
	MinSimilarity *float64 `yaml:"min_similarity"` // Minimum cosine similarity threshold (0.0-1.0)
}

// FilterCondition represents a single filter condition.
type FilterCondition struct {
	Column   string      `json:"column" yaml:"column"`
	Operator string      `json:"operator" yaml:"operator"`
	Value    interface{} `json:"value" yaml:"value"`
}

// Filter represents a collection of conditions with logical operators.
// Used for API request filters which must be parameterized for security.
type Filter struct {
	Conditions []FilterCondition `json:"conditions" yaml:"conditions"`
	Logic      string            `json:"logic,omitempty" yaml:"logic,omitempty"` // "AND" or "OR", default "AND"
}

// ConfigFilter represents a filter in pipeline configuration.
// It can be either a raw SQL string (for admin use) or a structured Filter.
type ConfigFilter struct {
	RawSQL     string  // Raw SQL WHERE clause fragment (admin-only)
	Structured *Filter // Structured filter with conditions
}

// UnmarshalYAML implements custom YAML unmarshaling for ConfigFilter.
// Allows filter to be specified as either a string or structured object.
func (cf *ConfigFilter) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try string first (raw SQL)
	var s string
	if err := unmarshal(&s); err == nil {
		cf.RawSQL = s
		return nil
	}

	// Try structured filter
	var f Filter
	if err := unmarshal(&f); err == nil {
		cf.Structured = &f
		return nil
	}

	return fmt.Errorf("filter must be a string or structured filter object")
}

// LLMConfig contains settings for an LLM provider.
type LLMConfig struct {
	Provider string            `yaml:"provider"`
	Model    string            `yaml:"model"`
	BaseURL  string            `yaml:"base_url"` // Optional custom base URL (e.g. for API gateways)
	Headers  map[string]string `yaml:"headers"`  // Per-LLM custom headers
}

// DefaultConfig returns a Config with sensible default values.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddress: "0.0.0.0",
			Port:          8080,
			TLS: TLSConfig{
				Enabled: false,
			},
		},
		Defaults: Defaults{
			TokenBudget: 1000,
			TopN:        10,
		},
	}
}
