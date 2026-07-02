//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package database

import (
	"strings"
	"testing"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

// Tests for filter functionality are in filter_test.go.

// TestBuildVectorSearchQuery_SelectsIDColumn verifies that the vector
// search query selects the configured id_column as "id". This is a
// regression test for the bug where the vector arm never selected an id,
// leaving vector SearchResults with an empty ID — which broke RRF fusion
// and id-based source resolution (citations).
func TestBuildVectorSearchQuery_SelectsIDColumn(t *testing.T) {
	table := config.TableSource{
		Table:        "public.chunks",
		TextColumn:   "content",
		VectorColumn: "embedding",
		IDColumn:     "doc_id",
	}

	query, _, err := buildVectorSearchQuery(
		[]float32{0.1, 0.2, 0.3}, table, 5, nil, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The configured id column must be selected and aliased as id.
	if !strings.Contains(query, `"doc_id"::text AS id`) {
		t.Errorf("query missing id column selection\nquery: %s", query)
	}
	// It must still select content and score.
	if !strings.Contains(query, "AS content") {
		t.Errorf("query missing content selection\nquery: %s", query)
	}
	if !strings.Contains(query, "AS score") {
		t.Errorf("query missing score selection\nquery: %s", query)
	}
	// The ROW_NUMBER fallback must NOT be used when an id column is set.
	if strings.Contains(query, "ROW_NUMBER") {
		t.Errorf("unexpected ROW_NUMBER fallback with id_column set\nquery: %s", query)
	}
}

// TestBuildVectorSearchQuery_NoIDColumnEmitsEmptyID verifies that when no
// id_column is configured, the vector search query emits an empty id
// (rather than a ROW_NUMBER() id). Row numbers from the vector query and
// the BM25 FetchDocuments query are assigned independently and do not
// identify the same document, so using them as an RRF key would falsely
// fuse unrelated documents. An empty id makes RRF and deduplication fall
// back to content-based keying, which is the only reliable cross-arm
// identity when no id_column exists.
func TestBuildVectorSearchQuery_NoIDColumnEmitsEmptyID(t *testing.T) {
	table := config.TableSource{
		Table:        "public.chunks",
		TextColumn:   "content",
		VectorColumn: "embedding",
		// IDColumn intentionally unset
	}

	query, _, err := buildVectorSearchQuery(
		[]float32{0.1, 0.2, 0.3}, table, 5, nil, nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, "''::text AS id") {
		t.Errorf("query should emit an empty id when no id_column is set\nquery: %s", query)
	}
	// A ROW_NUMBER() id would collide across arms and cause false fusion.
	if strings.Contains(query, "ROW_NUMBER") {
		t.Errorf("query must not use ROW_NUMBER for id (causes cross-arm false fusion)\nquery: %s", query)
	}
}

// TestBuildVectorSearchQuery_IDColumnWithFilterAndMinSimilarity verifies
// that selecting the id does not disturb the argument ordering
// ($1=vector, $2=limit, $3=minSimilarity, filters after) when a filter
// and minSimilarity are supplied together.
func TestBuildVectorSearchQuery_IDColumnWithFilterAndMinSimilarity(t *testing.T) {
	table := config.TableSource{
		Table:        "public.chunks",
		TextColumn:   "content",
		VectorColumn: "embedding",
		IDColumn:     "doc_id",
	}
	min := 0.8
	filter := &config.Filter{
		Conditions: []config.FilterCondition{
			{Column: "product", Operator: "=", Value: "pgEdge"},
		},
	}

	query, args, err := buildVectorSearchQuery(
		[]float32{0.1, 0.2, 0.3}, table, 5, filter, &min,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(query, `"doc_id"::text AS id`) {
		t.Errorf("query missing id column selection\nquery: %s", query)
	}
	// $3 is minSimilarity, filter starts at $4 — unchanged by the id fix.
	if !strings.Contains(query, `>= $3`) {
		t.Errorf("query missing minSimilarity at $3\nquery: %s", query)
	}
	if !strings.Contains(query, `"product" = $4`) {
		t.Errorf("query missing filter at $4\nquery: %s", query)
	}
	// args: [vector, topN=5, minSimilarity=0.8, "pgEdge"]
	if len(args) != 4 {
		t.Fatalf("arg count: got %d, want 4 — args: %v", len(args), args)
	}
	if args[1] != 5 || args[2] != 0.8 || args[3] != "pgEdge" {
		t.Errorf("unexpected args: %v", args)
	}
}
