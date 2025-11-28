//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	cfg, err := Load("../../testdata/configs/valid.yaml")
	if err != nil {
		t.Fatalf("failed to load valid config: %v", err)
	}

	// Check server config
	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.ListenAddress != "0.0.0.0" {
		t.Errorf("expected listen address 0.0.0.0, got %s", cfg.Server.ListenAddress)
	}

	// Check pipeline
	if len(cfg.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(cfg.Pipelines))
	}

	p := cfg.Pipelines[0]
	if p.Name != "test-pipeline" {
		t.Errorf("expected pipeline name 'test-pipeline', got '%s'", p.Name)
	}
	if p.TokenBudget != 2000 {
		t.Errorf("expected token budget 2000, got %d", p.TokenBudget)
	}
	if p.TopN != 15 {
		t.Errorf("expected top_n 15, got %d", p.TopN)
	}
}

func TestLoad_MinimalConfig(t *testing.T) {
	cfg, err := Load("../../testdata/configs/minimal.yaml")
	if err != nil {
		t.Fatalf("failed to load minimal config: %v", err)
	}

	// Check defaults are applied
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}

	p := cfg.Pipelines[0]
	if p.TokenBudget != 1000 {
		t.Errorf("expected default token budget 1000, got %d", p.TokenBudget)
	}
	if p.TopN != 10 {
		t.Errorf("expected default top_n 10, got %d", p.TopN)
	}
	if p.Database.Port != 5432 {
		t.Errorf("expected default database port 5432, got %d", p.Database.Port)
	}
	if p.Database.SSLMode != "prefer" {
		t.Errorf("expected default ssl_mode 'prefer', got '%s'", p.Database.SSLMode)
	}
}

func TestLoad_InvalidConfigs(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		errContains string
	}{
		{
			name:        "no pipelines",
			file:        "../../testdata/configs/invalid-no-pipelines.yaml",
			errContains: "at least one pipeline",
		},
		{
			name:        "invalid port",
			file:        "../../testdata/configs/invalid-port.yaml",
			errContains: "server.port",
		},
		{
			name:        "duplicate name",
			file:        "../../testdata/configs/invalid-duplicate-name.yaml",
			errContains: "duplicate pipeline name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(tt.file)
			if err == nil {
				t.Error("expected error, got nil")
				return
			}
			if !contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing '%s', got '%s'",
					tt.errContains, err.Error())
			}
		})
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Server.Port != 8080 {
		t.Errorf("expected default port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.ListenAddress != "0.0.0.0" {
		t.Errorf("expected default listen address '0.0.0.0', got '%s'",
			cfg.Server.ListenAddress)
	}
	if cfg.Defaults.TokenBudget != 1000 {
		t.Errorf("expected default token budget 1000, got %d",
			cfg.Defaults.TokenBudget)
	}
	if cfg.Defaults.TopN != 10 {
		t.Errorf("expected default top_n 10, got %d", cfg.Defaults.TopN)
	}
}

func TestValidation_MissingFields(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Port: 8080},
		Pipelines: []Pipeline{
			{
				Name: "test",
				Database: DatabaseConfig{
					// Missing host and database
					Port: 5432,
				},
				ColumnPairs: []ColumnPair{
					{
						// Missing table, text_column, vector_column
					},
				},
				EmbeddingLLM: LLMConfig{
					// Missing provider and model
				},
				RAGLLM: LLMConfig{
					// Missing provider and model
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}

	errStr := err.Error()
	expectedErrors := []string{
		"database.host",
		"database.database",
		"column_pairs[0].table",
		"column_pairs[0].text_column",
		"column_pairs[0].vector_column",
		"embedding_llm.provider",
		"embedding_llm.model",
		"rag_llm.provider",
		"rag_llm.model",
	}

	for _, expected := range expectedErrors {
		if !contains(errStr, expected) {
			t.Errorf("expected error to contain '%s', got '%s'", expected, errStr)
		}
	}
}

func TestValidation_InvalidLLMProvider(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Port: 8080},
		Pipelines: []Pipeline{
			{
				Name: "test",
				Database: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Database: "testdb",
				},
				ColumnPairs: []ColumnPair{
					{
						Table:        "docs",
						TextColumn:   "content",
						VectorColumn: "embedding",
					},
				},
				EmbeddingLLM: LLMConfig{
					Provider: "invalid-provider",
					Model:    "some-model",
				},
				RAGLLM: LLMConfig{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-20250514",
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid provider")
	}

	if !contains(err.Error(), "embedding_llm.provider") {
		t.Errorf("expected error about embedding_llm.provider, got: %s", err.Error())
	}
}

func TestExpandPath(t *testing.T) {
	homeDir, _ := os.UserHomeDir()

	tests := []struct {
		input    string
		expected string
	}{
		{"~/test", filepath.Join(homeDir, "test")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}

	for _, tt := range tests {
		result := expandPath(tt.input)
		if result != tt.expected {
			t.Errorf("expandPath(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
