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
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/identity"
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

// buildVectorSearchQuery constructs the SQL query and argument list for a
// vector similarity search. Extracted from VectorSearch for testability.
//
// Arg ordering: $1=vector, $2=limit. If minSimilarity is set it occupies $3
// and filters start at $4; otherwise filters start at $3.
func buildVectorSearchQuery(
	embedding []float32,
	table config.TableSource,
	topN int,
	filter *config.Filter,
	minSimilarity *float64,
) (string, []interface{}, error) {
	vectorCol := pgx.Identifier{table.VectorColumn}.Sanitize()

	nextParam := 3
	var extraArgs []interface{}
	if minSimilarity != nil {
		nextParam = 4
		extraArgs = append(extraArgs, *minSimilarity)
	}

	filterClause, filterArgs, err := buildFilterClause(table.Filter, filter, nextParam)
	if err != nil {
		return "", nil, fmt.Errorf("invalid filter: %w", err)
	}

	// Exclude rows with NULL vector column — NULL embeddings produce NULL scores
	// which cannot be scanned and are useless for similarity search.
	nullGuard := vectorCol + " IS NOT NULL"
	if filterClause == "" {
		filterClause = " WHERE " + nullGuard
	} else {
		filterClause = filterClause + " AND " + nullGuard
	}

	if minSimilarity != nil {
		simCondition := fmt.Sprintf("1 - (%s <=> $1::vector) >= $3", vectorCol)
		filterClause = filterClause + " AND " + simCondition
	}

	// Determine the ID expression. When an id_column is configured we select
	// it so vector results carry a stable id — this is what makes both search
	// arms key on the same id in RRF (correct fusion) and what makes vector
	// results usable for id-based source resolution (citations).
	//
	// When no id_column is configured there is no stable identifier. We must
	// NOT emit a ROW_NUMBER() id here: the vector query (ORDER BY ... LIMIT)
	// and the BM25 FetchDocuments query (full scan) number their rows
	// independently, so a row number from one arm does not identify the same
	// document as the same row number from the other. Using it as an RRF key
	// would falsely fuse unrelated documents. Emitting an empty id makes RRF
	// and deduplication fall back to keying on content, which is the only
	// reliable cross-arm identity when no id_column exists.
	var idExpr string
	if table.IDColumn != "" {
		idExpr = pgx.Identifier{table.IDColumn}.Sanitize() + "::text"
	} else {
		idExpr = "''::text"
	}

	query := fmt.Sprintf(`
		SELECT
			%s AS id,
			%s AS content,
			1 - (%s <=> $1::vector) AS score
		FROM %s%s
		ORDER BY %s <=> $1::vector
		LIMIT $2`,
		idExpr,
		pgx.Identifier{table.TextColumn}.Sanitize(),
		vectorCol,
		parseTableIdentifier(table.Table).Sanitize(),
		filterClause,
		vectorCol,
	)

	args := append([]interface{}{formatVector(embedding), topN}, extraArgs...)
	args = append(args, filterArgs...)
	return query, args, nil
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
	query, args, err := buildVectorSearchQuery(embedding, table, topN, filter, minSimilarity)
	if err != nil {
		return nil, err
	}

	var results []SearchResult
	err = p.withRows(ctx, query, args, func(rows pgx.Rows) error {
		for rows.Next() {
			var r SearchResult
			if err := rows.Scan(&r.ID, &r.Content, &r.Score); err != nil {
				return fmt.Errorf("failed to scan row: %w", err)
			}
			results = append(results, r)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("error iterating rows: %w", err)
		}
		return nil
	})
	if err != nil {
		// The identity errors are returned unwrapped in meaning so that
		// callers can classify them with errors.Is; wrapping them in
		// "vector search failed" would be true but useless, since the
		// search never ran.
		if errors.Is(err, identity.ErrRequired) {
			return nil, err
		}
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	return results, nil
}

// buildFetchDocumentsQuery constructs the SQL and argument list for the
// BM25 corpus read. Extracted from FetchDocuments for testability, in
// the same way as buildVectorSearchQuery.
//
// Arg ordering: filter arguments occupy $1..$n and the LIMIT takes
// $(n+1), so the placeholder index depends on how many arguments the
// filter contributed.
func buildFetchDocumentsQuery(
	table config.TableSource,
	filter *config.Filter,
	maxDocuments int,
) (string, []interface{}, error) {
	// Build filter clause combining config and request filters
	// Start at param index 1 (no initial params in this query)
	filterClause, args, err := buildFilterClause(table.Filter, filter, 1)
	if err != nil {
		return "", nil, fmt.Errorf("invalid filter: %w", err)
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

	limitPlaceholder := fmt.Sprintf("$%d", len(args)+1)
	args = append(args, maxDocuments)

	// Determine ID expression: use configured id_column, or ROW_NUMBER() fallback
	var query string
	if table.IDColumn != "" {
		// Use configured ID column
		query = fmt.Sprintf(`
		SELECT
			%s::text AS id,
			%s AS content
		FROM %s%s
		LIMIT %s`,
			pgx.Identifier{table.IDColumn}.Sanitize(),
			pgx.Identifier{table.TextColumn}.Sanitize(),
			parseTableIdentifier(table.Table).Sanitize(),
			filterClause,
			limitPlaceholder,
		)
	} else {
		// Fallback to ROW_NUMBER() for views or tables without explicit ID
		query = fmt.Sprintf(`
		SELECT
			ROW_NUMBER() OVER()::text AS id,
			%s AS content
		FROM %s%s
		LIMIT %s`,
			pgx.Identifier{table.TextColumn}.Sanitize(),
			parseTableIdentifier(table.Table).Sanitize(),
			filterClause,
			limitPlaceholder,
		)
	}

	return query, args, nil
}

// FetchDocuments fetches documents from a table for BM25 indexing,
// returning a map of document ID to content.
//
// maxDocuments bounds the read with a LIMIT. This arm has no
// server-side ranking to push down, so it necessarily reads rows and
// ranks them in memory; without the bound that is an unbounded read of
// the whole table on every request, and any caller able to reach the
// query endpoint can impose work proportional to table size rather
// than to request count.
//
// The LIMIT is deliberately not paired with an ORDER BY. Ordering would
// force the database to scan and sort the entire matching set before
// discarding all but the first n rows, which is the very cost being
// avoided; an unordered LIMIT lets it stop as soon as it has enough.
// The consequence is that when a table has more matching rows than the
// cap, the subset ranked by BM25 is an arbitrary one and may differ
// between requests. Callers should treat a returned count equal to
// maxDocuments as "possibly truncated" and say so, since keyword
// coverage is then partial.
//
// The filter parameter allows additional WHERE conditions from the API request.
func (p *Pool) FetchDocuments(
	ctx context.Context,
	table config.TableSource,
	filter *config.Filter,
	maxDocuments int,
) (map[string]string, error) {
	query, args, err := buildFetchDocumentsQuery(table, filter, maxDocuments)
	if err != nil {
		return nil, err
	}

	docs := make(map[string]string)
	if err := p.scanDocuments(ctx, query, args, docs); err != nil {
		return nil, err
	}

	return docs, nil
}

// scanDocuments runs an id/content query through withRows and collects
// the result into docs. Shared by FetchDocuments and
// FetchDocumentsByIDs so both arms go through the same identity-bearing
// path; a retrieval query that bypassed withRows would run as the
// service role and defeat the whole mechanism, so there is deliberately
// only one place that reads rows out of a corpus table.
func (p *Pool) scanDocuments(
	ctx context.Context,
	query string,
	args []interface{},
	docs map[string]string,
) error {
	err := p.withRows(ctx, query, args, func(rows pgx.Rows) error {
		for rows.Next() {
			var id, content string
			if err := rows.Scan(&id, &content); err != nil {
				return fmt.Errorf("failed to scan row: %w", err)
			}
			docs[id] = content
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("error iterating rows: %w", err)
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, identity.ErrRequired) {
			return err
		}
		return fmt.Errorf("failed to fetch documents: %w", err)
	}
	return nil
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

	docs := make(map[string]string)
	if err := p.scanDocuments(ctx, query, []interface{}{ids}, docs); err != nil {
		return nil, err
	}

	return docs, nil
}
