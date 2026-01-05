//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package bm25

import (
	"testing"
)

func TestBM25_New(t *testing.T) {
	bm := New()
	if bm.K1 != DefaultK1 {
		t.Errorf("expected K1 %f, got %f", DefaultK1, bm.K1)
	}
	if bm.B != DefaultB {
		t.Errorf("expected B %f, got %f", DefaultB, bm.B)
	}
}

func TestBM25_NewWithParams(t *testing.T) {
	bm := NewWithParams(1.5, 0.5)
	if bm.K1 != 1.5 {
		t.Errorf("expected K1 1.5, got %f", bm.K1)
	}
	if bm.B != 0.5 {
		t.Errorf("expected B 0.5, got %f", bm.B)
	}
}

func TestBM25_IDF(t *testing.T) {
	bm := New()
	bm.SetCorpusStats(100, 50)

	// Using Lucene-style IDF: log(1 + (N - df + 0.5) / (df + 0.5))
	// This always produces non-negative values
	tests := []struct {
		name    string
		docFreq int
		wantGT  float64 // Score should be greater than this
		wantLT  float64 // Score should be less than this
	}{
		{"rare term", 1, 4.0, 4.5},        // log(1 + 99.5/1.5) ≈ 4.21
		{"common term", 50, 0.5, 0.8},     // log(1 + 50.5/50.5) = log(2) ≈ 0.69
		{"very common term", 99, 0, 0.02}, // log(1 + 1.5/99.5) ≈ 0.015
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idf := bm.IDF(tt.docFreq)
			if idf <= tt.wantGT || idf >= tt.wantLT {
				t.Errorf("IDF(%d) = %f, want between %f and %f",
					tt.docFreq, idf, tt.wantGT, tt.wantLT)
			}
		})
	}
}

func TestBM25_IDF_EdgeCases(t *testing.T) {
	bm := New()

	// No corpus stats set
	if idf := bm.IDF(10); idf != 0 {
		t.Errorf("expected 0 for no corpus stats, got %f", idf)
	}

	bm.SetCorpusStats(100, 50)

	// Zero doc frequency
	if idf := bm.IDF(0); idf != 0 {
		t.Errorf("expected 0 for zero doc frequency, got %f", idf)
	}
}

func TestBM25_Score(t *testing.T) {
	bm := New()
	bm.SetCorpusStats(100, 50)

	// Test that score increases with term frequency
	score1 := bm.Score(1, 10, 50)
	score2 := bm.Score(5, 10, 50)
	if score2 <= score1 {
		t.Error("score should increase with term frequency")
	}

	// Test that score decreases with document length (for same tf)
	shortDoc := bm.Score(5, 10, 25)
	longDoc := bm.Score(5, 10, 100)
	if longDoc >= shortDoc {
		t.Error("score should decrease with document length")
	}

	// Test that rare terms have higher scores
	rareTermScore := bm.Score(1, 5, 50)
	commonTermScore := bm.Score(1, 50, 50)
	if commonTermScore >= rareTermScore {
		t.Error("rare terms should score higher than common terms")
	}
}

func TestBM25_Score_EdgeCases(t *testing.T) {
	bm := New()
	bm.SetCorpusStats(100, 50)

	if score := bm.Score(0, 10, 50); score != 0 {
		t.Errorf("expected 0 for zero tf, got %f", score)
	}

	if score := bm.Score(5, 0, 50); score != 0 {
		t.Errorf("expected 0 for zero doc freq, got %f", score)
	}
}

func TestBM25_ScoreDocument(t *testing.T) {
	bm := New()
	bm.SetCorpusStats(100, 50)

	queryTerms := map[string]int{"hello": 1, "world": 1}
	docTermFreqs := map[string]int{"hello": 2, "world": 1, "foo": 5}
	docFreqs := map[string]int{"hello": 10, "world": 50, "foo": 80}

	score := bm.ScoreDocument(queryTerms, docTermFreqs, docFreqs, 50)
	if score <= 0 {
		t.Error("expected positive score")
	}
}

func TestBM25_ScoreDocument_NoMatch(t *testing.T) {
	bm := New()
	bm.SetCorpusStats(100, 50)

	queryTerms := map[string]int{"hello": 1}
	docTermFreqs := map[string]int{"foo": 5, "bar": 3}
	docFreqs := map[string]int{"hello": 10, "foo": 80, "bar": 60}

	score := bm.ScoreDocument(queryTerms, docTermFreqs, docFreqs, 50)
	if score != 0 {
		t.Errorf("expected 0 for no matching terms, got %f", score)
	}
}
