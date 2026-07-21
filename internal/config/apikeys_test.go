//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package config

import "testing"

// TestLoadKeysForPipeline_RerankProviderKeyLoaded is a regression test
// for a live end-to-end bug found while verifying issue #22: a
// pipeline whose embedding_llm/rag_llm don't use Voyage, but whose
// rerank stage does, silently ended up with an empty Voyage key
// because LoadKeysForPipeline only inspected EmbeddingLLM.Provider and
// RAGLLM.Provider, never Rerank.Provider. NewRerankClient then failed
// pipeline creation with "Voyage API key not configured" even though
// VOYAGE_API_KEY was set.
func TestLoadKeysForPipeline_RerankProviderKeyLoaded(t *testing.T) {
	t.Setenv(EnvVoyageAPIKey, "vk-test")

	loader := NewAPIKeyLoader(APIKeysConfig{})
	p := Pipeline{
		EmbeddingLLM: LLMConfig{Provider: "ollama"},
		RAGLLM:       LLMConfig{Provider: "ollama"},
		Rerank:       RerankConfig{Provider: "voyage", Model: "rerank-2"},
	}

	keys, err := loader.LoadKeysForPipeline(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keys.Voyage != "vk-test" {
		t.Errorf("expected Voyage key to be loaded for rerank-only usage, got %q", keys.Voyage)
	}
}

// TestLoadKeysForPipeline_NoRerankDoesNotRequireVoyageKey verifies the
// converse: a pipeline with no rerank stage configured must not
// require (or attempt to load) a Voyage key, even if embedding/rag use
// other providers entirely.
func TestLoadKeysForPipeline_NoRerankDoesNotRequireVoyageKey(t *testing.T) {
	loader := NewAPIKeyLoader(APIKeysConfig{})
	p := Pipeline{
		EmbeddingLLM: LLMConfig{Provider: "ollama"},
		RAGLLM:       LLMConfig{Provider: "ollama"},
	}

	keys, err := loader.LoadKeysForPipeline(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keys.Voyage != "" {
		t.Errorf("expected no Voyage key without a rerank stage, got %q", keys.Voyage)
	}
}
