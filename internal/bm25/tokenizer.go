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
	"strings"
	"unicode"
)

// Tokenizer handles text tokenization for BM25 indexing.
type Tokenizer struct {
	stopWords map[string]bool
	lowercase bool
}

// DefaultStopWords contains common English stop words.
var DefaultStopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true,
	"at": true, "be": true, "by": true, "for": true, "from": true,
	"has": true, "he": true, "in": true, "is": true, "it": true,
	"its": true, "of": true, "on": true, "or": true, "that": true,
	"the": true, "to": true, "was": true, "were": true, "will": true,
	"with": true, "this": true, "but": true, "they": true, "have": true,
	"had": true, "what": true, "when": true, "where": true, "who": true,
	"which": true, "why": true, "how": true, "all": true, "each": true,
	"every": true, "both": true, "few": true, "more": true, "most": true,
	"other": true, "some": true, "such": true, "no": true, "not": true,
	"only": true, "same": true, "so": true, "than": true, "too": true,
	"very": true, "can": true, "just": true, "should": true, "now": true,
	"i": true, "you": true, "we": true, "me": true, "my": true,
	"your": true, "our": true, "their": true, "him": true, "her": true,
}

// NewTokenizer creates a new tokenizer with default settings.
func NewTokenizer() *Tokenizer {
	return &Tokenizer{
		stopWords: DefaultStopWords,
		lowercase: true,
	}
}

// NewTokenizerWithStopWords creates a tokenizer with custom stop words.
func NewTokenizerWithStopWords(stopWords map[string]bool) *Tokenizer {
	return &Tokenizer{
		stopWords: stopWords,
		lowercase: true,
	}
}

// Tokenize splits text into tokens, applying normalization.
func (t *Tokenizer) Tokenize(text string) []string {
	if t.lowercase {
		text = strings.ToLower(text)
	}

	// Split on non-alphanumeric characters
	var tokens []string
	var currentToken strings.Builder

	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			currentToken.WriteRune(r)
		} else if currentToken.Len() > 0 {
			token := currentToken.String()
			if t.isValidToken(token) {
				tokens = append(tokens, token)
			}
			currentToken.Reset()
		}
	}

	// Don't forget the last token
	if currentToken.Len() > 0 {
		token := currentToken.String()
		if t.isValidToken(token) {
			tokens = append(tokens, token)
		}
	}

	return tokens
}

// isValidToken checks if a token should be included.
func (t *Tokenizer) isValidToken(token string) bool {
	// Skip very short tokens
	if len(token) < 2 {
		return false
	}

	// Skip stop words
	if t.stopWords != nil && t.stopWords[token] {
		return false
	}

	return true
}

// TokenFrequencies returns a map of token to frequency count.
func (t *Tokenizer) TokenFrequencies(text string) map[string]int {
	tokens := t.Tokenize(text)
	freqs := make(map[string]int)

	for _, token := range tokens {
		freqs[token]++
	}

	return freqs
}

// TokenCount returns the total number of tokens in text.
func (t *Tokenizer) TokenCount(text string) int {
	return len(t.Tokenize(text))
}
