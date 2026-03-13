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
	"math"
	"testing"
)

// TestReciprocalRankFusion_EqualWeight verifies that equal vector and BM25
// weights (0.5) produce symmetric contributions and rank documents appearing
// in both result sets highest.
func TestReciprocalRankFusion_EqualWeight(t *testing.T) {
	vec := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 0.9},
		{ID: "b", Content: "doc-b", Score: 0.8},
	}
	bm25 := []SearchResult{
		{ID: "b", Content: "doc-b", Score: 5.0},
		{ID: "c", Content: "doc-c", Score: 3.0},
	}

	results := ReciprocalRankFusion(vec, bm25, 60, 0.5)

	// "b" appears in both lists — should have highest score
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	if results[0].ID != "b" {
		t.Errorf("expected top result 'b', got '%s'", results[0].ID)
	}

	// With equal weight, vector and BM25 contributions are the
	// same. "b" is rank 2 in vector (score = 0.5/(60+2)) and
	// rank 1 in BM25 (score = 0.5/(60+1)).
	expectedB := 0.5/(60+2) + 0.5/(60+1)
	if math.Abs(results[0].Score-expectedB) > 1e-9 {
		t.Errorf("expected score %f, got %f",
			expectedB, results[0].Score)
	}
}

// TestReciprocalRankFusion_VectorHeavy verifies that a high vector weight
// (0.8) causes the document ranked first by vector search to outscore the
// document ranked first by BM25.
func TestReciprocalRankFusion_VectorHeavy(t *testing.T) {
	vec := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 0.9},
		{ID: "b", Content: "doc-b", Score: 0.8},
	}
	bm25 := []SearchResult{
		{ID: "b", Content: "doc-b", Score: 5.0},
		{ID: "a", Content: "doc-a", Score: 3.0},
	}

	results := ReciprocalRankFusion(vec, bm25, 60, 0.8)

	// Both docs appear in both lists, but "a" is rank 1 in
	// vector (weighted heavily) and rank 2 in BM25. With 0.8
	// vector weight, "a" should outscore "b".
	//
	// a: 0.8/(60+1) + 0.2/(60+2) = 0.01311... + 0.00323...
	// b: 0.8/(60+2) + 0.2/(60+1) = 0.01290... + 0.00328...
	if results[0].ID != "a" {
		t.Errorf("expected 'a' ranked first with vector_weight=0.8, got '%s'",
			results[0].ID)
	}
}

// TestReciprocalRankFusion_BM25Heavy verifies that a low vector weight (0.2)
// causes the document ranked first by BM25 to outscore the document ranked
// first by vector search.
func TestReciprocalRankFusion_BM25Heavy(t *testing.T) {
	vec := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 0.9},
		{ID: "b", Content: "doc-b", Score: 0.8},
	}
	bm25 := []SearchResult{
		{ID: "b", Content: "doc-b", Score: 5.0},
		{ID: "a", Content: "doc-a", Score: 3.0},
	}

	results := ReciprocalRankFusion(vec, bm25, 60, 0.2)

	// With BM25-heavy weight, "b" (rank 1 in BM25) should win.
	// b: 0.2/(60+2) + 0.8/(60+1) = 0.00323... + 0.01311...
	// a: 0.2/(60+1) + 0.8/(60+2) = 0.00328... + 0.01290...
	if results[0].ID != "b" {
		t.Errorf("expected 'b' ranked first with vector_weight=0.2, got '%s'",
			results[0].ID)
	}
}

// TestHybridSearch_PassesWeight verifies that HybridSearch passes the
// vectorWeight parameter through to ReciprocalRankFusion, confirming that
// different weights produce different ranking orders.
func TestHybridSearch_PassesWeight(t *testing.T) {
	vec := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 0.9},
		{ID: "b", Content: "doc-b", Score: 0.8},
	}
	bm25 := []SearchResult{
		{ID: "b", Content: "doc-b", Score: 5.0},
		{ID: "a", Content: "doc-a", Score: 3.0},
	}

	// With vector weight 0.8, "a" should be first
	results := HybridSearch(vec, bm25, 2, 0.8)
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "a" {
		t.Errorf("expected 'a' first with weight 0.8, got '%s'",
			results[0].ID)
	}

	// With vector weight 0.2, "b" should be first
	results = HybridSearch(vec, bm25, 2, 0.2)
	if results[0].ID != "b" {
		t.Errorf("expected 'b' first with weight 0.2, got '%s'",
			results[0].ID)
	}
}

// TestReciprocalRankFusion_DefaultWeight verifies that an out-of-range
// vectorWeight (negative) is clamped to the default of 0.5.
func TestReciprocalRankFusion_DefaultWeight(t *testing.T) {
	vec := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 0.9},
	}
	bm25 := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 5.0},
	}

	results := ReciprocalRankFusion(vec, bm25, 60, -1.0)
	expectedA := 0.5/(60+1) + 0.5/(60+1)
	if math.Abs(results[0].Score-expectedA) > 1e-9 {
		t.Errorf("expected default 0.5 weight score %f, got %f",
			expectedA, results[0].Score)
	}
}

// TestReciprocalRankFusion_ClampAboveOne verifies that a vectorWeight
// greater than 1.0 is clamped to the default of 0.5.
func TestReciprocalRankFusion_ClampAboveOne(t *testing.T) {
	vec := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 0.9},
	}
	bm25 := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 5.0},
	}

	results := ReciprocalRankFusion(vec, bm25, 60, 1.5)
	expectedA := 0.5/(60+1) + 0.5/(60+1)
	if math.Abs(results[0].Score-expectedA) > 1e-9 {
		t.Errorf("expected clamped 0.5 weight score %f, got %f",
			expectedA, results[0].Score)
	}
}

// TestReciprocalRankFusion_PureVector verifies that vectorWeight=1.0
// skips the BM25 loop entirely, returning only documents from the
// vector result set with no BM25 contribution.
func TestReciprocalRankFusion_PureVector(t *testing.T) {
	vec := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 0.9},
		{ID: "b", Content: "doc-b", Score: 0.8},
	}
	bm25 := []SearchResult{
		{ID: "c", Content: "doc-c", Score: 5.0},
		{ID: "d", Content: "doc-d", Score: 3.0},
	}

	results := ReciprocalRankFusion(vec, bm25, 60, 1.0)

	// Only vector docs should appear; BM25-only docs excluded
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.ID] = true
	}
	if ids["c"] || ids["d"] {
		t.Error("BM25-only docs should not appear with vectorWeight=1.0")
	}
	if !ids["a"] || !ids["b"] {
		t.Error("expected vector docs 'a' and 'b'")
	}
}

// TestReciprocalRankFusion_PureBM25 verifies that vectorWeight=0.0
// skips the vector loop entirely, returning only documents from the
// BM25 result set with no vector contribution.
func TestReciprocalRankFusion_PureBM25(t *testing.T) {
	vec := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 0.9},
		{ID: "b", Content: "doc-b", Score: 0.8},
	}
	bm25 := []SearchResult{
		{ID: "c", Content: "doc-c", Score: 5.0},
		{ID: "d", Content: "doc-d", Score: 3.0},
	}

	results := ReciprocalRankFusion(vec, bm25, 60, 0.0)

	// Only BM25 docs should appear; vector-only docs excluded
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.ID] = true
	}
	if ids["a"] || ids["b"] {
		t.Error("vector-only docs should not appear with vectorWeight=0.0")
	}
	if !ids["c"] || !ids["d"] {
		t.Error("expected BM25 docs 'c' and 'd'")
	}
}

// TestReciprocalRankFusion_DisjointResults verifies correct fusion when
// vector and BM25 result sets have no overlapping documents.
func TestReciprocalRankFusion_DisjointResults(t *testing.T) {
	vec := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 0.9},
		{ID: "b", Content: "doc-b", Score: 0.8},
	}
	bm25 := []SearchResult{
		{ID: "c", Content: "doc-c", Score: 5.0},
		{ID: "d", Content: "doc-d", Score: 3.0},
	}

	results := ReciprocalRankFusion(vec, bm25, 60, 0.5)

	// All 4 docs should appear since there's no overlap
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// Each doc should have a single-source score only
	for _, r := range results {
		if r.VecRank > 0 && r.BM25Rank > 0 {
			t.Errorf("doc '%s' should not appear in both rankings", r.ID)
		}
	}
}

// TestReciprocalRankFusion_EmptyVectorResults verifies that when the
// vector result set is empty, only BM25 results are returned.
func TestReciprocalRankFusion_EmptyVectorResults(t *testing.T) {
	vec := []SearchResult{}
	bm25 := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 5.0},
		{ID: "b", Content: "doc-b", Score: 3.0},
	}

	results := ReciprocalRankFusion(vec, bm25, 60, 0.5)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "a" {
		t.Errorf("expected 'a' first, got '%s'", results[0].ID)
	}
	if results[0].VecRank != 0 {
		t.Error("VecRank should be 0 for BM25-only result")
	}
}

// TestReciprocalRankFusion_EmptyBM25Results verifies that when the
// BM25 result set is empty, only vector results are returned.
func TestReciprocalRankFusion_EmptyBM25Results(t *testing.T) {
	vec := []SearchResult{
		{ID: "a", Content: "doc-a", Score: 0.9},
		{ID: "b", Content: "doc-b", Score: 0.8},
	}
	bm25 := []SearchResult{}

	results := ReciprocalRankFusion(vec, bm25, 60, 0.5)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "a" {
		t.Errorf("expected 'a' first, got '%s'", results[0].ID)
	}
	if results[0].BM25Rank != 0 {
		t.Error("BM25Rank should be 0 for vector-only result")
	}
}

// TestReciprocalRankFusion_BothEmpty verifies that when both result
// sets are empty, an empty slice is returned.
func TestReciprocalRankFusion_BothEmpty(t *testing.T) {
	results := ReciprocalRankFusion(
		[]SearchResult{}, []SearchResult{}, 60, 0.5,
	)

	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}
