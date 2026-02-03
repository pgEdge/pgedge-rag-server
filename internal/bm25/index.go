//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package bm25

import (
	"sort"
	"sync"
)

// Document represents an indexed document.
type Document struct {
	ID        string
	Content   string
	Length    int            // Number of tokens
	TermFreqs map[string]int // Term frequencies
}

// SearchResult represents a BM25 search result.
type SearchResult struct {
	ID      string
	Content string
	Score   float64
}

// Index is an in-memory BM25 index.
type Index struct {
	mu        sync.RWMutex
	tokenizer *Tokenizer
	scorer    *BM25
	docs      map[string]*Document // docID -> Document
	docFreqs  map[string]int       // term -> document frequency
	totalDocs int
	totalLen  int // Total length of all documents (for avg calculation)
}

// NewIndex creates a new BM25 index.
func NewIndex() *Index {
	return &Index{
		tokenizer: NewTokenizer(),
		scorer:    New(),
		docs:      make(map[string]*Document),
		docFreqs:  make(map[string]int),
	}
}

// NewIndexWithParams creates a new BM25 index with custom parameters.
func NewIndexWithParams(k1, b float64) *Index {
	return &Index{
		tokenizer: NewTokenizer(),
		scorer:    NewWithParams(k1, b),
		docs:      make(map[string]*Document),
		docFreqs:  make(map[string]int),
	}
}

// AddDocument adds a document to the index.
func (idx *Index) AddDocument(id, content string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// Tokenize and get term frequencies
	termFreqs := idx.tokenizer.TokenFrequencies(content)
	docLen := 0
	for _, freq := range termFreqs {
		docLen += freq
	}

	doc := &Document{
		ID:        id,
		Content:   content,
		Length:    docLen,
		TermFreqs: termFreqs,
	}

	// Update document frequencies
	for term := range termFreqs {
		idx.docFreqs[term]++
	}

	idx.docs[id] = doc
	idx.totalDocs++
	idx.totalLen += docLen

	// Update scorer stats
	idx.updateScorerStats()
}

// AddDocuments adds multiple documents to the index.
func (idx *Index) AddDocuments(docs map[string]string) {
	for id, content := range docs {
		idx.AddDocument(id, content)
	}
}

// updateScorerStats updates the BM25 scorer with current corpus statistics.
func (idx *Index) updateScorerStats() {
	avgDL := 0.0
	if idx.totalDocs > 0 {
		avgDL = float64(idx.totalLen) / float64(idx.totalDocs)
	}
	idx.scorer.SetCorpusStats(idx.totalDocs, avgDL)
}

// Search performs a BM25 search and returns the top-N results.
func (idx *Index) Search(query string, topN int) []SearchResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if idx.totalDocs == 0 {
		return nil
	}

	// Tokenize query
	queryTermFreqs := idx.tokenizer.TokenFrequencies(query)
	if len(queryTermFreqs) == 0 {
		return nil
	}

	// Score each document
	type scoredDoc struct {
		id      string
		content string
		score   float64
	}

	var scored []scoredDoc
	for id, doc := range idx.docs {
		score := idx.scorer.ScoreDocument(
			queryTermFreqs,
			doc.TermFreqs,
			idx.docFreqs,
			doc.Length,
		)
		if score > 0 {
			scored = append(scored, scoredDoc{
				id:      id,
				content: doc.Content,
				score:   score,
			})
		}
	}

	// Sort by score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Return top N
	results := make([]SearchResult, 0, min(topN, len(scored)))
	for i := 0; i < len(scored) && i < topN; i++ {
		results = append(results, SearchResult{
			ID:      scored[i].id,
			Content: scored[i].content,
			Score:   scored[i].score,
		})
	}

	return results
}

// Clear removes all documents from the index.
func (idx *Index) Clear() {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	idx.docs = make(map[string]*Document)
	idx.docFreqs = make(map[string]int)
	idx.totalDocs = 0
	idx.totalLen = 0
}

// Size returns the number of documents in the index.
func (idx *Index) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.totalDocs
}

// GetDocument returns a document by ID.
func (idx *Index) GetDocument(id string) (*Document, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	doc, ok := idx.docs[id]
	return doc, ok
}
