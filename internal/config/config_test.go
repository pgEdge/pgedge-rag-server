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
				Tables: []TableSource{
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
		"tables[0].table",
		"tables[0].text_column",
		"tables[0].vector_column",
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
				Tables: []TableSource{
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

func TestLoad_LLMDefaults(t *testing.T) {
	cfg, err := Load("../../testdata/configs/llm-defaults.yaml")
	if err != nil {
		t.Fatalf("failed to load llm-defaults config: %v", err)
	}

	// Check that defaults were applied
	if cfg.Defaults.EmbeddingLLM.Provider != "openai" {
		t.Errorf("expected defaults embedding provider 'openai', got '%s'",
			cfg.Defaults.EmbeddingLLM.Provider)
	}
	if cfg.Defaults.EmbeddingLLM.Model != "text-embedding-3-small" {
		t.Errorf("expected defaults embedding model 'text-embedding-3-small', got '%s'",
			cfg.Defaults.EmbeddingLLM.Model)
	}

	// Check pipeline that inherits all defaults
	p1 := cfg.Pipelines[0]
	if p1.Name != "inherits-all" {
		t.Fatalf("expected first pipeline 'inherits-all', got '%s'", p1.Name)
	}
	if p1.EmbeddingLLM.Provider != "openai" {
		t.Errorf("pipeline '%s': expected embedding provider 'openai', got '%s'",
			p1.Name, p1.EmbeddingLLM.Provider)
	}
	if p1.EmbeddingLLM.Model != "text-embedding-3-small" {
		t.Errorf("pipeline '%s': expected embedding model 'text-embedding-3-small', got '%s'",
			p1.Name, p1.EmbeddingLLM.Model)
	}
	if p1.RAGLLM.Provider != "anthropic" {
		t.Errorf("pipeline '%s': expected rag provider 'anthropic', got '%s'",
			p1.Name, p1.RAGLLM.Provider)
	}
	if p1.RAGLLM.Model != "claude-sonnet-4-20250514" {
		t.Errorf("pipeline '%s': expected rag model 'claude-sonnet-4-20250514', got '%s'",
			p1.Name, p1.RAGLLM.Model)
	}
	if p1.TokenBudget != 3000 {
		t.Errorf("pipeline '%s': expected token_budget 3000, got %d",
			p1.Name, p1.TokenBudget)
	}

	// Check pipeline that overrides rag_llm
	p2 := cfg.Pipelines[1]
	if p2.Name != "overrides-rag" {
		t.Fatalf("expected second pipeline 'overrides-rag', got '%s'", p2.Name)
	}
	if p2.EmbeddingLLM.Provider != "openai" {
		t.Errorf("pipeline '%s': expected embedding provider 'openai', got '%s'",
			p2.Name, p2.EmbeddingLLM.Provider)
	}
	if p2.RAGLLM.Provider != "openai" {
		t.Errorf("pipeline '%s': expected rag provider 'openai', got '%s'",
			p2.Name, p2.RAGLLM.Provider)
	}
	if p2.RAGLLM.Model != "gpt-4o-mini" {
		t.Errorf("pipeline '%s': expected rag model 'gpt-4o-mini', got '%s'",
			p2.Name, p2.RAGLLM.Model)
	}

	// Check pipeline that overrides only model (inherits provider)
	p3 := cfg.Pipelines[2]
	if p3.Name != "overrides-model-only" {
		t.Fatalf("expected third pipeline 'overrides-model-only', got '%s'", p3.Name)
	}
	if p3.EmbeddingLLM.Provider != "openai" {
		t.Errorf("pipeline '%s': expected embedding provider 'openai', got '%s'",
			p3.Name, p3.EmbeddingLLM.Provider)
	}
	if p3.EmbeddingLLM.Model != "text-embedding-3-large" {
		t.Errorf("pipeline '%s': expected embedding model 'text-embedding-3-large', got '%s'",
			p3.Name, p3.EmbeddingLLM.Model)
	}
}

func TestLoad_InvalidLLMDefaults(t *testing.T) {
	_, err := Load("../../testdata/configs/invalid-llm-defaults.yaml")
	if err == nil {
		t.Fatal("expected validation error for invalid LLM defaults")
	}

	if !contains(err.Error(), "defaults.embedding_llm.provider") {
		t.Errorf("expected error about defaults.embedding_llm.provider, got: %s",
			err.Error())
	}
}

func TestValidation_DefaultsLLMProviderOnly(t *testing.T) {
	// Test that when defaults has provider but no model, it's an error
	cfg := &Config{
		Server: ServerConfig{Port: 8080},
		Defaults: Defaults{
			TokenBudget: 1000,
			TopN:        10,
			EmbeddingLLM: LLMConfig{
				Provider: "openai",
				// Missing model
			},
		},
		Pipelines: []Pipeline{
			{
				Name: "test",
				Database: DatabaseConfig{
					Host:     "localhost",
					Port:     5432,
					Database: "testdb",
				},
				Tables: []TableSource{
					{
						Table:        "docs",
						TextColumn:   "content",
						VectorColumn: "embedding",
					},
				},
				// Will inherit defaults
			},
		},
	}

	// Apply defaults before validation
	applyDefaults(cfg)

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for defaults with provider but no model")
	}

	if !contains(err.Error(), "defaults.embedding_llm.model") {
		t.Errorf("expected error about defaults.embedding_llm.model, got: %s",
			err.Error())
	}
}

func TestApplyDefaults_APIKeysCascade(t *testing.T) {
	// Test the API keys cascade: pipeline -> defaults -> global
	cfg := &Config{
		Server: ServerConfig{Port: 8080},
		APIKeys: APIKeysConfig{
			Anthropic: "/global/anthropic.key",
			OpenAI:    "/global/openai.key",
			Voyage:    "/global/voyage.key",
		},
		Defaults: Defaults{
			TokenBudget: 1000,
			TopN:        10,
			APIKeys: APIKeysConfig{
				OpenAI: "/defaults/openai.key", // Override global
				// Anthropic not set - should cascade from global
			},
			EmbeddingLLM: LLMConfig{Provider: "openai", Model: "text-embedding-3-small"},
			RAGLLM:       LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
		},
		Pipelines: []Pipeline{
			{
				Name: "pipeline1",
				Database: DatabaseConfig{
					Host:     "localhost",
					Database: "testdb",
				},
				Tables: []TableSource{
					{Table: "docs", TextColumn: "content", VectorColumn: "embedding"},
				},
				APIKeys: APIKeysConfig{
					// No API keys set - should inherit from defaults/global
				},
			},
			{
				Name: "pipeline2",
				Database: DatabaseConfig{
					Host:     "localhost",
					Database: "testdb",
				},
				Tables: []TableSource{
					{Table: "docs", TextColumn: "content", VectorColumn: "embedding"},
				},
				APIKeys: APIKeysConfig{
					Anthropic: "/pipeline/anthropic.key", // Override all
				},
			},
		},
	}

	applyDefaults(cfg)

	// Pipeline 1: Should cascade from defaults (OpenAI) and global (Anthropic, Voyage)
	p1 := cfg.Pipelines[0]
	if p1.APIKeys.OpenAI != "/defaults/openai.key" {
		t.Errorf("pipeline1 OpenAI: expected '/defaults/openai.key', got '%s'",
			p1.APIKeys.OpenAI)
	}
	if p1.APIKeys.Anthropic != "/global/anthropic.key" {
		t.Errorf("pipeline1 Anthropic: expected '/global/anthropic.key', got '%s'",
			p1.APIKeys.Anthropic)
	}
	if p1.APIKeys.Voyage != "/global/voyage.key" {
		t.Errorf("pipeline1 Voyage: expected '/global/voyage.key', got '%s'",
			p1.APIKeys.Voyage)
	}

	// Pipeline 2: Should use pipeline-specific Anthropic, defaults OpenAI, global Voyage
	p2 := cfg.Pipelines[1]
	if p2.APIKeys.Anthropic != "/pipeline/anthropic.key" {
		t.Errorf("pipeline2 Anthropic: expected '/pipeline/anthropic.key', got '%s'",
			p2.APIKeys.Anthropic)
	}
	if p2.APIKeys.OpenAI != "/defaults/openai.key" {
		t.Errorf("pipeline2 OpenAI: expected '/defaults/openai.key', got '%s'",
			p2.APIKeys.OpenAI)
	}
	if p2.APIKeys.Voyage != "/global/voyage.key" {
		t.Errorf("pipeline2 Voyage: expected '/global/voyage.key', got '%s'",
			p2.APIKeys.Voyage)
	}
}

func TestLoad_SystemPrompt(t *testing.T) {
	cfg, err := Load("../../testdata/configs/system-prompt.yaml")
	if err != nil {
		t.Fatalf("failed to load system-prompt config: %v", err)
	}

	if len(cfg.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d", len(cfg.Pipelines))
	}

	p := cfg.Pipelines[0]
	if p.Name != "test-with-system-prompt" {
		t.Errorf("expected pipeline name 'test-with-system-prompt', got '%s'", p.Name)
	}

	// Verify system_prompt was parsed correctly
	if p.SystemPrompt == "" {
		t.Fatal("expected SystemPrompt to be set, but it was empty")
	}

	// Verify it contains expected content
	expectedPhrases := []string{
		"Ellie",
		"pgEdge documentation",
		"concise and accurate",
	}

	for _, phrase := range expectedPhrases {
		if !contains(p.SystemPrompt, phrase) {
			t.Errorf("SystemPrompt should contain '%s', got: %s", phrase, p.SystemPrompt)
		}
	}
}

func TestApplyDefaults_SearchConfig(t *testing.T) {
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
				Tables: []TableSource{
					{Table: "docs", TextColumn: "content", VectorColumn: "embedding"},
				},
				EmbeddingLLM: LLMConfig{Provider: "openai", Model: "text-embedding-3-small"},
				RAGLLM:       LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
				// No Search config set - should get defaults
			},
		},
	}

	applyDefaults(cfg)

	p := cfg.Pipelines[0]
	if p.Search.HybridEnabled == nil {
		t.Fatal("expected HybridEnabled to be set")
	}
	if *p.Search.HybridEnabled != true {
		t.Errorf("expected HybridEnabled to be true, got %v", *p.Search.HybridEnabled)
	}
	if p.Search.VectorWeight == nil {
		t.Fatal("expected VectorWeight to be set")
	}
	if *p.Search.VectorWeight != 0.5 {
		t.Errorf("expected VectorWeight to be 0.5, got %v", *p.Search.VectorWeight)
	}
}

func TestValidation_InvalidVectorWeight(t *testing.T) {
	invalidWeight := 1.5
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
				Tables: []TableSource{
					{Table: "docs", TextColumn: "content", VectorColumn: "embedding"},
				},
				EmbeddingLLM: LLMConfig{Provider: "openai", Model: "text-embedding-3-small"},
				RAGLLM:       LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
				Search: SearchConfig{
					VectorWeight: &invalidWeight,
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid vector_weight")
	}

	if !contains(err.Error(), "search.vector_weight") {
		t.Errorf("expected error about search.vector_weight, got: %s", err.Error())
	}
}

func TestValidation_ValidVectorWeight(t *testing.T) {
	tests := []float64{0.0, 0.5, 1.0}
	for _, w := range tests {
		weight := w
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
					Tables: []TableSource{
						{Table: "docs", TextColumn: "content", VectorColumn: "embedding"},
					},
					EmbeddingLLM: LLMConfig{Provider: "openai", Model: "text-embedding-3-small"},
					RAGLLM:       LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
					Search: SearchConfig{
						VectorWeight: &weight,
					},
				},
			},
		}

		err := cfg.Validate()
		if err != nil {
			t.Errorf("unexpected validation error for vector_weight=%v: %v", w, err)
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
