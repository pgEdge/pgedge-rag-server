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
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

// parseTableIdentifier splits a table name into schema and table parts.
// Supports formats: "table", "schema.table"
func parseTableIdentifier(table string) pgx.Identifier {
	parts := strings.Split(table, ".")
	return pgx.Identifier(parts)
}

// formatVector converts a float32 slice to pgvector string format [x,y,z,...].
func formatVector(embedding []float32) string {
	strs := make([]string, len(embedding))
	for i, v := range embedding {
		strs[i] = fmt.Sprintf("%g", v)
	}
	return "[" + strings.Join(strs, ",") + "]"
}

// SearchResult represents a single search result.
type SearchResult struct {
	ID         string                 `json:"id,omitempty"`
	Content    string                 `json:"content"`
	Score      float64                `json:"score"`
	SourceInfo map[string]interface{} `json:"source_info,omitempty"`
}

// VectorSearch performs a vector similarity search using pgvector.
// Returns results ordered by similarity (highest first).
// The filter parameter allows additional WHERE conditions from the API request.
// If minSimilarity is non-nil, results below that cosine similarity are excluded.
func (p *Pool) VectorSearch(
	ctx context.Context,
	embedding []float32,
	table config.TableSource,
	topN int,
	filter *config.Filter,
	minSimilarity *float64,
) ([]SearchResult, error) {
	// Determine starting param index:
	// $1=vector, $2=limit, optionally $3=min_similarity
	nextParam := 3
	var extraArgs []interface{}
	if minSimilarity != nil {
		nextParam = 4
		extraArgs = append(extraArgs, *minSimilarity)
	}

	// Build filter clause combining config and request filters
	filterClause, filterArgs, err := buildFilterClause(table.Filter, filter, nextParam)
	if err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	// Add min_similarity condition to the filter clause
	if minSimilarity != nil {
		simCondition := fmt.Sprintf(
			"1 - (%s <=> $1::vector) >= $3",
			pgx.Identifier{table.VectorColumn}.Sanitize(),
		)
		if filterClause == "" {
			filterClause = " WHERE " + simCondition
		} else {
			filterClause = filterClause + " AND " + simCondition
		}
	}

	// Build the query using cosine distance operator from pgvector
	// The <=> operator returns cosine distance, so we subtract from 1 for similarity
	query := fmt.Sprintf(`
		SELECT
			%s AS content,
			1 - (%s <=> $1::vector) AS score
		FROM %s%s
		ORDER BY %s <=> $1::vector
		LIMIT $2`,
		pgx.Identifier{table.TextColumn}.Sanitize(),
		pgx.Identifier{table.VectorColumn}.Sanitize(),
		parseTableIdentifier(table.Table).Sanitize(),
		filterClause,
		pgx.Identifier{table.VectorColumn}.Sanitize(),
	)

	// Combine vector embedding, topN, optional min_similarity, and filter args
	args := append([]interface{}{formatVector(embedding), topN}, extraArgs...)
	args = append(args, filterArgs...)
	rows, err := p.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Content, &r.Score); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		results = append(results, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}

// FetchDocuments fetches all documents from a table for BM25 indexing.
// Returns a map of document ID to content.
// The filter parameter allows additional WHERE conditions from the API request.
func (p *Pool) FetchDocuments(
	ctx context.Context,
	table config.TableSource,
	filter *config.Filter,
) (map[string]string, error) {
	// Build filter clause combining config and request filters
	// Start at param index 1 (no initial params in this query)
	filterClause, filterArgs, err := buildFilterClause(table.Filter, filter, 1)
	if err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	// Build base WHERE clause for non-null content
	baseCondition := fmt.Sprintf("%s IS NOT NULL",
		pgx.Identifier{table.TextColumn}.Sanitize())

	// Combine filter with IS NOT NULL condition
	if filterClause == "" {
		filterClause = " WHERE " + baseCondition
	} else {
		filterClause = filterClause + " AND " + baseCondition
	}

	// Determine ID expression: use configured id_column, or ROW_NUMBER() fallback
	var query string
	if table.IDColumn != "" {
		// Use configured ID column
		query = fmt.Sprintf(`
		SELECT
			%s::text AS id,
			%s AS content
		FROM %s%s`,
			pgx.Identifier{table.IDColumn}.Sanitize(),
			pgx.Identifier{table.TextColumn}.Sanitize(),
			parseTableIdentifier(table.Table).Sanitize(),
			filterClause,
		)
	} else {
		// Fallback to ROW_NUMBER() for views or tables without explicit ID
		query = fmt.Sprintf(`
		SELECT
			ROW_NUMBER() OVER()::text AS id,
			%s AS content
		FROM %s%s`,
			pgx.Identifier{table.TextColumn}.Sanitize(),
			parseTableIdentifier(table.Table).Sanitize(),
			filterClause,
		)
	}

	rows, err := p.pool.Query(ctx, query, filterArgs...)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch documents: %w", err)
	}
	defer rows.Close()

	docs := make(map[string]string)
	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		docs[id] = content
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return docs, nil
}

// FetchDocumentsByIDs fetches documents by their IDs.
// When IDColumn is configured, it uses that column for filtering.
// When using ROW_NUMBER() fallback (no IDColumn), this function cannot
// reliably fetch by ID and returns an empty result.
func (p *Pool) FetchDocumentsByIDs(
	ctx context.Context,
	table config.TableSource,
	ids []string,
) (map[string]string, error) {
	if len(ids) == 0 {
		return make(map[string]string), nil
	}

	// If no ID column is configured, we can't reliably fetch by ID
	// (ROW_NUMBER is not stable across queries)
	if table.IDColumn == "" {
		return make(map[string]string), nil
	}

	query := fmt.Sprintf(`
		SELECT
			%s::text AS id,
			%s AS content
		FROM %s
		WHERE %s::text = ANY($1::text[])`,
		pgx.Identifier{table.IDColumn}.Sanitize(),
		pgx.Identifier{table.TextColumn}.Sanitize(),
		parseTableIdentifier(table.Table).Sanitize(),
		pgx.Identifier{table.IDColumn}.Sanitize(),
	)

	rows, err := p.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch documents: %w", err)
	}
	defer rows.Close()

	docs := make(map[string]string)
	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		docs[id] = content
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return docs, nil
}
