//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Environment variable names for API keys.
const (
	EnvAnthropicAPIKey = "ANTHROPIC_API_KEY"
	EnvOpenAIAPIKey    = "OPENAI_API_KEY"
	EnvVoyageAPIKey    = "VOYAGE_API_KEY"
)

// Default API key file paths (relative to home directory).
const (
	DefaultAnthropicKeyFile = ".anthropic-api-key"
	DefaultOpenAIKeyFile    = ".openai-api-key"
	DefaultVoyageKeyFile    = ".voyage-api-key"
)

// LoadedKeys holds all loaded API keys.
type LoadedKeys struct {
	Anthropic string
	OpenAI    string
	Voyage    string
}

// APIKeyLoader handles loading API keys from configured paths, environment
// variables, or default file locations.
type APIKeyLoader struct {
	config APIKeysConfig
}

// NewAPIKeyLoader creates a new API key loader with the given configuration.
func NewAPIKeyLoader(cfg APIKeysConfig) *APIKeyLoader {
	return &APIKeyLoader{config: cfg}
}

// LoadAnthropicKey loads the Anthropic API key.
func (l *APIKeyLoader) LoadAnthropicKey() (string, error) {
	return l.loadKey(
		l.config.Anthropic,
		EnvAnthropicAPIKey,
		DefaultAnthropicKeyFile,
		"Anthropic",
	)
}

// LoadOpenAIKey loads the OpenAI API key.
func (l *APIKeyLoader) LoadOpenAIKey() (string, error) {
	return l.loadKey(
		l.config.OpenAI,
		EnvOpenAIAPIKey,
		DefaultOpenAIKeyFile,
		"OpenAI",
	)
}

// LoadVoyageKey loads the Voyage API key.
func (l *APIKeyLoader) LoadVoyageKey() (string, error) {
	return l.loadKey(
		l.config.Voyage,
		EnvVoyageAPIKey,
		DefaultVoyageKeyFile,
		"Voyage",
	)
}

// loadKey loads an API key with the following priority:
// 1. Configured file path (if specified in config)
// 2. Environment variable
// 3. Default file location (~/.provider-api-key)
func (l *APIKeyLoader) loadKey(
	configPath, envVar, defaultFile, providerName string,
) (string, error) {
	// Priority 1: Configured file path
	if configPath != "" {
		path := expandKeyPath(configPath)
		return readKeyFile(path, providerName)
	}

	// Priority 2: Environment variable
	if key := os.Getenv(envVar); key != "" {
		return key, nil
	}

	// Priority 3: Default file location
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	path := filepath.Join(homeDir, defaultFile)

	// Check if default file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf(
			"%s API key not found: set %s environment variable or create %s",
			providerName, envVar, path)
	}

	return readKeyFile(path, providerName)
}

// readKeyFile reads an API key from a file.
func readKeyFile(path, providerName string) (string, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("%s API key file not found: %s", providerName, path)
	}

	// Read the key
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s API key: %w", providerName, err)
	}

	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("%s API key file is empty: %s", providerName, path)
	}

	return key, nil
}

// expandKeyPath expands ~ to the user's home directory.
func expandKeyPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// LoadRequiredKeys loads only the API keys required by the given pipelines.
// Deprecated: Use LoadKeysForPipeline for per-pipeline API key loading.
func (l *APIKeyLoader) LoadRequiredKeys(pipelines []Pipeline) (*LoadedKeys, error) {
	keys := &LoadedKeys{}
	needed := make(map[string]bool)

	// Determine which providers are needed
	for _, p := range pipelines {
		needed[strings.ToLower(p.EmbeddingLLM.Provider)] = true
		needed[strings.ToLower(p.RAGLLM.Provider)] = true
	}

	// Load required keys
	if needed["anthropic"] {
		key, err := l.LoadAnthropicKey()
		if err != nil {
			return nil, err
		}
		keys.Anthropic = key
	}

	if needed["openai"] {
		key, err := l.LoadOpenAIKey()
		if err != nil {
			return nil, err
		}
		keys.OpenAI = key
	}

	if needed["voyage"] {
		key, err := l.LoadVoyageKey()
		if err != nil {
			return nil, err
		}
		keys.Voyage = key
	}

	// Ollama doesn't require an API key

	return keys, nil
}

// LoadKeysForPipeline loads only the API keys required by a single pipeline.
// The loader should be initialized with the pipeline's effective API key config
// (already cascaded from pipeline -> defaults -> global).
func (l *APIKeyLoader) LoadKeysForPipeline(pipeline Pipeline) (*LoadedKeys, error) {
	keys := &LoadedKeys{}
	needed := make(map[string]bool)

	// Determine which providers are needed for this pipeline
	needed[strings.ToLower(pipeline.EmbeddingLLM.Provider)] = true
	needed[strings.ToLower(pipeline.RAGLLM.Provider)] = true

	// Load required keys
	if needed["anthropic"] {
		key, err := l.LoadAnthropicKey()
		if err != nil {
			return nil, err
		}
		keys.Anthropic = key
	}

	if needed["openai"] {
		key, err := l.LoadOpenAIKey()
		if err != nil {
			return nil, err
		}
		keys.OpenAI = key
	}

	if needed["voyage"] {
		key, err := l.LoadVoyageKey()
		if err != nil {
			return nil, err
		}
		keys.Voyage = key
	}

	// Ollama doesn't require an API key

	return keys, nil
}
