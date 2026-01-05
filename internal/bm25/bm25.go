//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package bm25 provides BM25 text search scoring.
package bm25

import (
	"math"
)

// DefaultK1 is the default term frequency saturation parameter.
// Higher values mean term frequency has more impact.
const DefaultK1 = 1.2

// DefaultB is the default document length normalization parameter.
// B=0 means no normalization, B=1 means full normalization.
const DefaultB = 0.75

// BM25 implements the BM25 (Best Matching 25) ranking function.
type BM25 struct {
	K1       float64 // Term frequency saturation (default 1.2)
	B        float64 // Document length normalization (default 0.75)
	AvgDL    float64 // Average document length
	DocCount int     // Total number of documents
}

// New creates a new BM25 scorer with default parameters.
func New() *BM25 {
	return &BM25{
		K1: DefaultK1,
		B:  DefaultB,
	}
}

// NewWithParams creates a BM25 scorer with custom parameters.
func NewWithParams(k1, b float64) *BM25 {
	return &BM25{
		K1: k1,
		B:  b,
	}
}

// SetCorpusStats sets the corpus statistics needed for scoring.
func (bm *BM25) SetCorpusStats(docCount int, avgDocLength float64) {
	bm.DocCount = docCount
	bm.AvgDL = avgDocLength
}

// IDF calculates the Inverse Document Frequency for a term.
// Uses the Lucene/Elasticsearch variant of the BM25 IDF formula:
//
//	IDF(t) = log(1 + (N - df(t) + 0.5) / (df(t) + 0.5))
//
// where N is the total number of documents and df(t) is the
// document frequency of term t.
//
// This variant ensures IDF is always non-negative, unlike the
// standard formula which can produce negative values for common terms.
func (bm *BM25) IDF(docFreq int) float64 {
	if bm.DocCount == 0 || docFreq == 0 {
		return 0
	}

	n := float64(bm.DocCount)
	df := float64(docFreq)

	// Lucene-style IDF: log(1 + (N - df + 0.5) / (df + 0.5))
	// This ensures IDF is always non-negative
	numerator := n - df + 0.5
	denominator := df + 0.5

	return math.Log(1 + numerator/denominator)
}

// Score calculates the BM25 score for a term in a document.
//
// Parameters:
//   - tf: term frequency in the document
//   - docFreq: number of documents containing the term
//   - docLen: length of the document (in terms)
//
// Returns the BM25 score component for this term.
func (bm *BM25) Score(tf, docFreq, docLen int) float64 {
	if tf == 0 || docFreq == 0 || bm.DocCount == 0 {
		return 0
	}

	idf := bm.IDF(docFreq)

	// Term frequency component with saturation
	tfFloat := float64(tf)
	docLenFloat := float64(docLen)

	// Length normalization factor
	lengthNorm := 1 - bm.B + bm.B*(docLenFloat/bm.AvgDL)

	// BM25 term frequency score
	tfScore := (tfFloat * (bm.K1 + 1)) / (tfFloat + bm.K1*lengthNorm)

	return idf * tfScore
}

// ScoreDocument calculates the total BM25 score for a document
// given a query.
//
// Parameters:
//   - queryTerms: map of query term -> term frequency in query
//   - docTermFreqs: map of term -> term frequency in document
//   - docFreqs: map of term -> document frequency in corpus
//   - docLen: length of document (in terms)
//
// Returns the total BM25 score for the document.
func (bm *BM25) ScoreDocument(
	queryTerms map[string]int,
	docTermFreqs map[string]int,
	docFreqs map[string]int,
	docLen int,
) float64 {
	var score float64

	for term := range queryTerms {
		tf := docTermFreqs[term]
		df := docFreqs[term]
		score += bm.Score(tf, df, docLen)
	}

	return score
}
