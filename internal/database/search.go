//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
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
// The filter parameter allows additional SQL WHERE conditions to be applied.
func (p *Pool) VectorSearch(
	ctx context.Context,
	embedding []float32,
	columnPair config.ColumnPair,
	topN int,
	filter *config.Filter,
) ([]SearchResult, error) {
	// Build filter clause combining config and request filters
	filterClause, filterArgs, err := buildFilterClause(columnPair.Filter, filter)
	if err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
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
		pgx.Identifier{columnPair.TextColumn}.Sanitize(),
		pgx.Identifier{columnPair.VectorColumn}.Sanitize(),
		parseTableIdentifier(columnPair.Table).Sanitize(),
		filterClause,
		pgx.Identifier{columnPair.VectorColumn}.Sanitize(),
	)

	// Combine vector embedding and topN with filter args
	args := append([]interface{}{formatVector(embedding), topN}, filterArgs...)
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
// The filter parameter allows additional SQL WHERE conditions to be applied.
func (p *Pool) FetchDocuments(
	ctx context.Context,
	columnPair config.ColumnPair,
	filter *config.Filter,
) (map[string]string, error) {
	// Build filter clause combining config and request filters
	filterClause, filterArgs, err := buildFilterClause(columnPair.Filter, filter)
	if err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	// Build base WHERE clause for non-null content
	baseCondition := fmt.Sprintf("%s IS NOT NULL",
		pgx.Identifier{columnPair.TextColumn}.Sanitize())

	// Combine filter with IS NOT NULL condition
	if filterClause == "" {
		filterClause = " WHERE " + baseCondition
	} else {
		filterClause = filterClause + " AND " + baseCondition
	}

	// Try to use ctid as a unique identifier if no ID column exists
	query := fmt.Sprintf(`
		SELECT
			ctid::text AS id,
			%s AS content
		FROM %s%s`,
		pgx.Identifier{columnPair.TextColumn}.Sanitize(),
		parseTableIdentifier(columnPair.Table).Sanitize(),
		filterClause,
	)

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

// FetchDocumentsByIDs fetches documents by their IDs (ctids).
func (p *Pool) FetchDocumentsByIDs(
	ctx context.Context,
	columnPair config.ColumnPair,
	ids []string,
) (map[string]string, error) {
	if len(ids) == 0 {
		return make(map[string]string), nil
	}

	query := fmt.Sprintf(`
		SELECT
			ctid::text AS id,
			%s AS content
		FROM %s
		WHERE ctid = ANY($1::tid[])`,
		pgx.Identifier{columnPair.TextColumn}.Sanitize(),
		parseTableIdentifier(columnPair.Table).Sanitize(),
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
