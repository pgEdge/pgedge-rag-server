//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package bm25

import (
	"testing"
)

func TestIndex_AddDocument(t *testing.T) {
	idx := NewIndex()

	idx.AddDocument("1", "hello world")
	idx.AddDocument("2", "goodbye world")

	if idx.Size() != 2 {
		t.Errorf("expected size 2, got %d", idx.Size())
	}

	doc, ok := idx.GetDocument("1")
	if !ok {
		t.Fatal("expected to find document 1")
	}
	if doc.ID != "1" {
		t.Errorf("expected ID 1, got %s", doc.ID)
	}
	if doc.Content != "hello world" {
		t.Errorf("expected content 'hello world', got %s", doc.Content)
	}
}

func TestIndex_AddDocuments(t *testing.T) {
	idx := NewIndex()

	docs := map[string]string{
		"1": "hello world",
		"2": "goodbye world",
		"3": "hello again",
	}
	idx.AddDocuments(docs)

	if idx.Size() != 3 {
		t.Errorf("expected size 3, got %d", idx.Size())
	}
}

func TestIndex_Search(t *testing.T) {
	idx := NewIndex()

	docs := map[string]string{
		"1": "PostgreSQL is a powerful relational database",
		"2": "MySQL is another popular database",
		"3": "MongoDB is a NoSQL document database",
		"4": "Redis is an in-memory data store",
	}
	idx.AddDocuments(docs)

	results := idx.Search("PostgreSQL database", 10)

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	// PostgreSQL doc should be first since it matches both terms
	if results[0].ID != "1" {
		t.Errorf("expected doc 1 to be first, got %s", results[0].ID)
	}

	// All database docs should be in results
	foundDB := 0
	for _, r := range results {
		if r.ID == "1" || r.ID == "2" || r.ID == "3" {
			foundDB++
		}
	}
	if foundDB < 3 {
		t.Errorf("expected to find 3 database docs, found %d", foundDB)
	}
}

func TestIndex_Search_NoResults(t *testing.T) {
	idx := NewIndex()

	docs := map[string]string{
		"1": "hello world",
		"2": "goodbye world",
	}
	idx.AddDocuments(docs)

	results := idx.Search("postgresql database", 10)
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

func TestIndex_Search_EmptyQuery(t *testing.T) {
	idx := NewIndex()
	idx.AddDocument("1", "hello world")

	results := idx.Search("", 10)
	if len(results) > 0 {
		t.Errorf("expected no results for empty query, got %d", len(results))
	}
}

func TestIndex_Search_EmptyIndex(t *testing.T) {
	idx := NewIndex()

	results := idx.Search("hello", 10)
	if len(results) > 0 {
		t.Error("expected no results for empty index")
	}
}

func TestIndex_Search_TopN(t *testing.T) {
	idx := NewIndex()

	// Add many documents containing "database"
	for i := 1; i <= 10; i++ {
		idx.AddDocument(string(rune('0'+i)), "database document number")
	}

	results := idx.Search("database", 3)
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestIndex_Clear(t *testing.T) {
	idx := NewIndex()

	idx.AddDocument("1", "hello world")
	idx.AddDocument("2", "goodbye world")

	if idx.Size() != 2 {
		t.Fatal("expected size 2 before clear")
	}

	idx.Clear()

	if idx.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", idx.Size())
	}

	_, ok := idx.GetDocument("1")
	if ok {
		t.Error("expected document 1 to not exist after clear")
	}
}

func TestIndex_ScoresAreSorted(t *testing.T) {
	idx := NewIndex()

	docs := map[string]string{
		"1": "database database database",        // Should score highest
		"2": "database database",                 // Second highest
		"3": "database",                          // Third
		"4": "unrelated content with no matches", // No match
	}
	idx.AddDocuments(docs)

	results := idx.Search("database", 10)

	// Check that scores are in descending order
	for i := 0; i < len(results)-1; i++ {
		if results[i].Score < results[i+1].Score {
			t.Errorf("results not sorted: score[%d]=%f < score[%d]=%f",
				i, results[i].Score, i+1, results[i+1].Score)
		}
	}
}

func TestIndex_NewIndexWithParams(t *testing.T) {
	idx := NewIndexWithParams(1.5, 0.5)

	idx.AddDocument("1", "hello world")

	if idx.Size() != 1 {
		t.Errorf("expected size 1, got %d", idx.Size())
	}

	// Verify custom parameters are used
	if idx.scorer.K1 != 1.5 {
		t.Errorf("expected K1 1.5, got %f", idx.scorer.K1)
	}
	if idx.scorer.B != 0.5 {
		t.Errorf("expected B 0.5, got %f", idx.scorer.B)
	}
}
