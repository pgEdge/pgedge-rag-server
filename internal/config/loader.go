//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	// ConfigFileName is the default configuration file name.
	ConfigFileName = "pgedge-rag-server.yaml"

	// SystemConfigPath is the system-wide configuration path.
	SystemConfigPath = "/etc/pgedge/" + ConfigFileName
)

// Load loads the configuration from the specified path, or searches
// default locations if path is empty.
//
// Search order:
//  1. Explicit path (if provided)
//  2. /etc/pgedge/pgedge-rag-server.yaml
//  3. pgedge-rag-server.yaml in the binary's directory
func Load(path string) (*Config, error) {
	configPath, err := findConfigFile(path)
	if err != nil {
		return nil, err
	}

	return loadFromFile(configPath)
}

// findConfigFile finds the configuration file using the search order.
func findConfigFile(explicitPath string) (string, error) {
	// If explicit path provided, use it
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err != nil {
			return "", fmt.Errorf("config file not found: %s", explicitPath)
		}
		return explicitPath, nil
	}

	// Search order for config file
	searchPaths := []string{
		SystemConfigPath,
		getBinaryDirConfigPath(),
	}

	for _, p := range searchPaths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("no configuration file found; searched: %v", searchPaths)
}

// getBinaryDirConfigPath returns the path to config file in the binary's
// directory.
func getBinaryDirConfigPath() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}

	// Resolve symlinks to get the actual binary location
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return ""
	}

	return filepath.Join(filepath.Dir(executable), ConfigFileName)
}

// loadFromFile loads and parses the configuration from a YAML file.
func loadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Start with defaults
	cfg := DefaultConfig()

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Apply defaults to pipelines
	applyDefaults(cfg)

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// applyDefaults applies default values to pipelines where not specified.
func applyDefaults(cfg *Config) {
	for i := range cfg.Pipelines {
		p := &cfg.Pipelines[i]

		// Apply token budget default
		if p.TokenBudget == 0 {
			p.TokenBudget = cfg.Defaults.TokenBudget
		}

		// Apply top_n default
		if p.TopN == 0 {
			p.TopN = cfg.Defaults.TopN
		}

		// Apply embedding LLM defaults
		if p.EmbeddingLLM.Provider == "" {
			p.EmbeddingLLM.Provider = cfg.Defaults.EmbeddingLLM.Provider
		}
		if p.EmbeddingLLM.Model == "" {
			p.EmbeddingLLM.Model = cfg.Defaults.EmbeddingLLM.Model
		}
		if p.EmbeddingLLM.BaseURL == "" {
			p.EmbeddingLLM.BaseURL = cfg.Defaults.EmbeddingLLM.BaseURL
		}

		// Apply RAG LLM defaults
		if p.RAGLLM.Provider == "" {
			p.RAGLLM.Provider = cfg.Defaults.RAGLLM.Provider
		}
		if p.RAGLLM.Model == "" {
			p.RAGLLM.Model = cfg.Defaults.RAGLLM.Model
		}
		if p.RAGLLM.BaseURL == "" {
			p.RAGLLM.BaseURL = cfg.Defaults.RAGLLM.BaseURL
		}

		// Apply API key defaults (cascade: pipeline -> defaults -> global)
		if p.APIKeys.Anthropic == "" {
			if cfg.Defaults.APIKeys.Anthropic != "" {
				p.APIKeys.Anthropic = cfg.Defaults.APIKeys.Anthropic
			} else {
				p.APIKeys.Anthropic = cfg.APIKeys.Anthropic
			}
		}
		if p.APIKeys.OpenAI == "" {
			if cfg.Defaults.APIKeys.OpenAI != "" {
				p.APIKeys.OpenAI = cfg.Defaults.APIKeys.OpenAI
			} else {
				p.APIKeys.OpenAI = cfg.APIKeys.OpenAI
			}
		}
		if p.APIKeys.Voyage == "" {
			if cfg.Defaults.APIKeys.Voyage != "" {
				p.APIKeys.Voyage = cfg.Defaults.APIKeys.Voyage
			} else {
				p.APIKeys.Voyage = cfg.APIKeys.Voyage
			}
		}
		if p.APIKeys.Gemini == "" {
			if cfg.Defaults.APIKeys.Gemini != "" {
				p.APIKeys.Gemini = cfg.Defaults.APIKeys.Gemini
			} else {
				p.APIKeys.Gemini = cfg.APIKeys.Gemini
			}
		}

		// Apply LLM header defaults (cascade: defaults -> pipeline)
		if len(cfg.Defaults.LLMHeaders) > 0 && len(p.LLMHeaders) == 0 {
			p.LLMHeaders = make(map[string]string, len(cfg.Defaults.LLMHeaders))
			for k, v := range cfg.Defaults.LLMHeaders {
				p.LLMHeaders[k] = v
			}
		}

		// Apply database port default
		if len(p.Database.Hosts) == 0 && p.Database.Port == 0 {
			p.Database.Port = 5432
		}

		// Apply database ssl_mode default
		if p.Database.SSLMode == "" {
			p.Database.SSLMode = "prefer"
		}

		// Apply per-host port defaults
		for j := range p.Database.Hosts {
			if p.Database.Hosts[j].Port == 0 {
				p.Database.Hosts[j].Port = 5432
			}
		}

		// Default target_session_attrs for multi-host configs only
		if len(p.Database.Hosts) > 0 && p.Database.TargetSessionAttrs == "" {
			p.Database.TargetSessionAttrs = "prefer-standby"
		}

		// Apply search config defaults
		if p.Search.HybridEnabled == nil {
			defaultHybrid := true
			p.Search.HybridEnabled = &defaultHybrid
		}
		if p.Search.VectorWeight == nil {
			defaultWeight := 0.5
			p.Search.VectorWeight = &defaultWeight
		}
	}
}
