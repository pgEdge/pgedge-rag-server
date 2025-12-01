//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package config handles configuration loading and validation for the
// pgEdge RAG Server.
package config

// Config is the root configuration structure for the server.
type Config struct {
	Server    ServerConfig  `yaml:"server"`
	APIKeys   APIKeysConfig `yaml:"api_keys"`
	Defaults  Defaults      `yaml:"defaults"`
	Pipelines []Pipeline    `yaml:"pipelines"`
}

// APIKeysConfig contains paths to files containing API keys for LLM providers.
// If not specified, keys are loaded from environment variables or default
// file locations (~/.anthropic-api-key, ~/.openai-api-key, ~/.voyage-api-key).
type APIKeysConfig struct {
	Anthropic string `yaml:"anthropic"` // Path to file containing Anthropic API key
	OpenAI    string `yaml:"openai"`    // Path to file containing OpenAI API key
	Voyage    string `yaml:"voyage"`    // Path to file containing Voyage API key
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	ListenAddress string    `yaml:"listen_address"`
	Port          int       `yaml:"port"`
	TLS           TLSConfig `yaml:"tls"`
}

// TLSConfig contains TLS/HTTPS settings.
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Defaults contains default values that can be overridden per-pipeline.
type Defaults struct {
	TokenBudget int `yaml:"token_budget"`
	TopN        int `yaml:"top_n"`
}

// Pipeline defines a single RAG pipeline configuration.
type Pipeline struct {
	Name         string         `yaml:"name"`
	Description  string         `yaml:"description"`
	Database     DatabaseConfig `yaml:"database"`
	ColumnPairs  []ColumnPair   `yaml:"column_pairs"`
	EmbeddingLLM LLMConfig      `yaml:"embedding_llm"`
	RAGLLM       LLMConfig      `yaml:"rag_llm"`
	TokenBudget  int            `yaml:"token_budget"`
	TopN         int            `yaml:"top_n"`
}

// DatabaseConfig contains PostgreSQL connection settings.
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	SSLMode  string `yaml:"ssl_mode"`

	// Certificate-based authentication
	SSLCert   string `yaml:"ssl_cert"`
	SSLKey    string `yaml:"ssl_key"`
	SSLRootCA string `yaml:"ssl_root_ca"`
}

// ColumnPair defines a text column and its corresponding vector column
// for hybrid search.
type ColumnPair struct {
	Table        string `yaml:"table"`
	TextColumn   string `yaml:"text_column"`
	VectorColumn string `yaml:"vector_column"`
	Filter       string `yaml:"filter"` // Optional SQL WHERE clause fragment
}

// LLMConfig contains settings for an LLM provider.
type LLMConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
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
