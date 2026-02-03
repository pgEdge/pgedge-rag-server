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
	"reflect"
	"testing"
)

func TestTokenizer_Tokenize(t *testing.T) {
	tok := NewTokenizer()

	tests := []struct {
		name   string
		input  string
		expect []string
	}{
		{
			name:   "simple text",
			input:  "hello world",
			expect: []string{"hello", "world"},
		},
		{
			name:   "with punctuation",
			input:  "Hello, World!",
			expect: []string{"hello", "world"},
		},
		{
			name:   "with numbers",
			input:  "version 2.0 released",
			expect: []string{"version", "released"},
		},
		{
			name:   "stop words removed",
			input:  "the quick brown fox jumps over the lazy dog",
			expect: []string{"quick", "brown", "fox", "jumps", "over", "lazy", "dog"},
		},
		{
			name:   "empty string",
			input:  "",
			expect: nil,
		},
		{
			name:   "only stop words",
			input:  "the and or",
			expect: nil,
		},
		{
			name:   "mixed case",
			input:  "PostgreSQL Database",
			expect: []string{"postgresql", "database"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tok.Tokenize(tt.input)
			if !reflect.DeepEqual(result, tt.expect) {
				t.Errorf("Tokenize(%q) = %v, want %v", tt.input, result, tt.expect)
			}
		})
	}
}

func TestTokenizer_TokenFrequencies(t *testing.T) {
	tok := NewTokenizer()

	input := "hello world hello database world world"
	freqs := tok.TokenFrequencies(input)

	expected := map[string]int{
		"hello":    2,
		"world":    3,
		"database": 1,
	}

	if !reflect.DeepEqual(freqs, expected) {
		t.Errorf("TokenFrequencies = %v, want %v", freqs, expected)
	}
}

func TestTokenizer_TokenCount(t *testing.T) {
	tok := NewTokenizer()

	tests := []struct {
		input    string
		expected int
	}{
		{"hello world", 2},
		{"", 0},
		{"the and or", 0}, // All stop words
		{"hello the world", 2},
	}

	for _, tt := range tests {
		count := tok.TokenCount(tt.input)
		if count != tt.expected {
			t.Errorf("TokenCount(%q) = %d, want %d", tt.input, count, tt.expected)
		}
	}
}

func TestTokenizer_CustomStopWords(t *testing.T) {
	customStopWords := map[string]bool{
		"hello": true,
		"world": true,
	}
	tok := NewTokenizerWithStopWords(customStopWords)

	result := tok.Tokenize("hello world database")
	expected := []string{"database"}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}
