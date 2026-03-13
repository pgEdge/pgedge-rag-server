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
	"sort"
)

// DefaultRRFConstant is the default k constant for RRF ranking.
// A value of 60 is commonly used in practice.
const DefaultRRFConstant = 60

// RRFResult represents a result after RRF fusion.
type RRFResult struct {
	ID       string
	Content  string
	Score    float64
	VecRank  int // Rank in vector search results (0 if not present)
	BM25Rank int // Rank in BM25 results (0 if not present)
}

// ReciprocalRankFusion combines results from vector and BM25 searches
// using Reciprocal Rank Fusion (RRF).
//
// RRF formula: score = sum(weight / (k + rank)) for each ranking
// where k is a constant (default 60), rank is 1-indexed, and weight is
// vectorWeight for vector results or (1 - vectorWeight) for BM25 results.
//
// The function returns results sorted by combined RRF score (highest first).
func ReciprocalRankFusion(
	vectorResults []SearchResult,
	bm25Results []SearchResult,
	k float64,
	vectorWeight float64,
) []RRFResult {
	if k <= 0 {
		k = DefaultRRFConstant
	}
	if vectorWeight < 0 || vectorWeight > 1 {
		vectorWeight = 0.5
	}
	bm25Weight := 1.0 - vectorWeight

	// Map to accumulate scores and track ranks
	resultMap := make(map[string]*RRFResult)

	// Process vector results
	if vectorWeight > 0 {
		for i, r := range vectorResults {
			rank := i + 1 // 1-indexed
			key := r.Content
			if r.ID != "" {
				key = r.ID
			}

			if existing, ok := resultMap[key]; ok {
				existing.Score += vectorWeight / (k + float64(rank))
				existing.VecRank = rank
			} else {
				resultMap[key] = &RRFResult{
					ID:      r.ID,
					Content: r.Content,
					Score:   vectorWeight / (k + float64(rank)),
					VecRank: rank,
				}
			}
		}
	}

	// Process BM25 results
	if bm25Weight > 0 {
		for i, r := range bm25Results {
			rank := i + 1 // 1-indexed
			key := r.Content
			if r.ID != "" {
				key = r.ID
			}

			if existing, ok := resultMap[key]; ok {
				existing.Score += bm25Weight / (k + float64(rank))
				existing.BM25Rank = rank
			} else {
				resultMap[key] = &RRFResult{
					ID:       r.ID,
					Content:  r.Content,
					Score:    bm25Weight / (k + float64(rank)),
					BM25Rank: rank,
				}
			}
		}
	}

	// Convert map to slice
	results := make([]RRFResult, 0, len(resultMap))
	for _, r := range resultMap {
		results = append(results, *r)
	}

	// Sort by score (highest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

// HybridSearch combines vector and BM25 search results using RRF.
// This is a convenience function that takes search results and returns
// the top-N fused results.
func HybridSearch(
	vectorResults []SearchResult,
	bm25Results []SearchResult,
	topN int,
	vectorWeight float64,
) []SearchResult {
	rrfResults := ReciprocalRankFusion(vectorResults, bm25Results, DefaultRRFConstant, vectorWeight)

	// Convert back to SearchResult and limit to topN
	results := make([]SearchResult, 0, min(topN, len(rrfResults)))
	for i, r := range rrfResults {
		if i >= topN {
			break
		}
		results = append(results, SearchResult{
			ID:      r.ID,
			Content: r.Content,
			Score:   r.Score,
		})
	}

	return results
}
