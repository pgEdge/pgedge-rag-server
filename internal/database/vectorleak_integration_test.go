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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

// Corpus shape for the side-channel test. The numbers matter: the
// effect appears when one identity's rows are a small minority of what
// the index contains, because the graph walk spends its candidate
// budget (hnsw.ef_search, 40 by default) on rows the caller may not see
// before it has found enough she may.
const (
	leakMajorityRows = 20000
	leakMinorityRows = 50
	leakVectorDim    = 16
	leakWantedRows   = 10
)

// seedLeakCorpus builds a table where one identity owns almost
// everything and another owns very little, with an HNSW index over the
// whole thing and a policy keyed on the claims parameter.
//
// Rows are generated inside the database rather than inserted one by
// one from Go: twenty thousand round trips would dominate the test's
// runtime and prove nothing extra.
func seedLeakCorpus(
	t *testing.T,
	admin *pgxpool.Pool,
	schema, role, claimsSetting string,
) string {
	t.Helper()

	q := pgx.Identifier{schema, "chunks"}.Sanitize()

	exec(t, admin, fmt.Sprintf(`CREATE TABLE %s (
		id bigserial PRIMARY KEY,
		owner text NOT NULL,
		content text NOT NULL,
		embedding vector(%d))`, q, leakVectorDim))

	insert := fmt.Sprintf(`
		INSERT INTO %s (owner, content, embedding)
		SELECT $1, $1 || ' chunk ' || g,
		       (SELECT '[' || string_agg(random()::text, ',') || ']'
		          FROM generate_series(1, %d))::vector
		  FROM generate_series(1, $2) g`, q, leakVectorDim)

	exec(t, admin, insert, "alice", leakMajorityRows)
	exec(t, admin, insert, "bob", leakMinorityRows)

	exec(t, admin, fmt.Sprintf(
		"CREATE INDEX ON %s USING hnsw (embedding vector_cosine_ops)", q))
	exec(t, admin, fmt.Sprintf("ANALYZE %s", q))

	exec(t, admin, fmt.Sprintf(
		"ALTER TABLE %s ENABLE ROW LEVEL SECURITY", q))
	exec(t, admin, fmt.Sprintf(
		`CREATE POLICY own_rows ON %s FOR SELECT
		   USING (owner = current_setting(%s, true)::json->>'sub')`,
		q, quoteLiteral(claimsSetting)))
	exec(t, admin, fmt.Sprintf("GRANT SELECT ON %s TO %s",
		q, pgx.Identifier{role}.Sanitize()))

	return schema + ".chunks"
}

// probeVector is the query vector both halves of the test use. Any
// vector works; a fixed one keeps the test deterministic given a fixed
// corpus.
func probeVector() []float32 {
	v := make([]float32, leakVectorDim)
	for i := range v {
		v[i] = 0.5
	}
	return v
}

// explainAnalyze runs a query under EXPLAIN ANALYZE through the
// identity-bearing path, and returns the plan text together with the
// number of rows the query actually produced.
//
// Plan and row count come from the same execution deliberately. Reading
// the plan with one statement and the row count with another looks
// equivalent but is not: PostgreSQL may plan a parameterised query
// differently between a custom and a generic plan, so the two
// statements can disagree about whether the index was used — and an
// assertion that pairs "the plan says index scan" with "this other
// query returned n rows" is then asserting nothing in particular. One
// statement cannot disagree with itself.
func explainAnalyze(
	t *testing.T,
	pool *Pool,
	ctx context.Context,
	query string,
	args []interface{},
) (plan string, actualRows int) {
	t.Helper()

	var lines []string
	err := pool.withRows(ctx,
		"EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY OFF) "+query, args,
		func(rows pgx.Rows) error {
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					return err
				}
				lines = append(lines, line)
			}
			return rows.Err()
		})
	if err != nil {
		t.Fatalf("EXPLAIN ANALYZE failed: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("EXPLAIN ANALYZE returned no plan")
	}

	// The topmost node is the Limit, and its actual row count is what the
	// caller would have received.
	rows, err := parseActualRows(lines[0])
	if err != nil {
		t.Fatalf("could not read the row count from plan line %q: %v", lines[0], err)
	}

	return strings.Join(lines, "\n"), rows
}

// parseActualRows extracts n from an "(actual rows=n loops=...)"
// annotation on a plan line.
func parseActualRows(line string) (int, error) {
	const marker = "actual rows="
	idx := strings.Index(line, marker)
	if idx < 0 {
		return 0, fmt.Errorf("no %q annotation", marker)
	}

	rest := line[idx+len(marker):]
	end := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
	if end == 0 {
		return 0, fmt.Errorf("no digits after %q", marker)
	}
	if end < 0 {
		end = len(rest)
	}

	return strconv.Atoi(rest[:end])
}

// usesIndexScan reports whether a plan answers the query from an index
// rather than by scanning and sorting.
func usesIndexScan(plan string) bool {
	return strings.Contains(plan, "Index Scan") ||
		strings.Contains(plan, "Index Only Scan")
}

// TestSharedVectorIndexLeaksAcrossIdentities documents, as an executable
// assertion, the exposure that per-identity retrieval creates when the
// vector index is shared — and proves that the default configuration
// closes it.
//
// pgvector applies row-level security as a filter on top of an index
// scan that has already chosen its candidates from the whole corpus. So
// the minority identity asks for ten nearest chunks and gets fewer,
// often none, because the scan's budget was spent on rows she may not
// see. No forbidden row is ever returned — the filtering works. But the
// number of rows she gets back, and how long the query took, are
// functions of how many rows she may NOT see lie near her query vector.
// A caller who chooses query vectors can use that to map another
// tenant's corpus in embedding space, and embedding inversion turns a
// position in that space back into approximate text.
//
// Filtering harder does not help, because the filtering is what
// produces the signal. What removes it is not using the shared
// approximate structure, which is why AllowSharedVectorIndex defaults to
// false and the identity-bearing transaction disables index scans.
//
// If this test ever starts failing because the shared-index arm returns
// a full result set, that is worth investigating rather than deleting:
// it would most likely mean the corpus shape drifted, not that pgvector
// stopped behaving this way.
func TestSharedVectorIndexLeaksAcrossIdentities(t *testing.T) {
	admin := adminPool(t)
	if !hasPGVector(t, admin) {
		t.Skip("pgvector is not installed; skipping vector index side-channel test")
	}

	role, password := serviceRole(t, admin)
	schema := testSchema(t, admin, role)

	claimsSetting := config.DefaultClaimsSetting
	table := seedLeakCorpus(t, admin, schema, role, claimsSetting)

	source := config.TableSource{
		Table:        table,
		TextColumn:   "content",
		VectorColumn: "embedding",
		IDColumn:     "id",
	}

	ctx := callerContext(bobClaims)

	t.Run("shared index starves the minority identity", func(t *testing.T) {
		cfg := config.IdentityConfig{
			Enabled:                true,
			AllowSharedVectorIndex: true,
		}
		pool := servicePool(t, role, password, cfg, 2)

		query, args, err := buildVectorSearchQuery(probeVector(), source,
			leakWantedRows, nil, nil)
		if err != nil {
			t.Fatalf("failed to build the search query: %v", err)
		}

		// Plan and row count from one execution, through the same
		// identity-bearing path the real query uses, so the planner
		// settings this configuration applies are in force.
		plan, delivered := explainAnalyze(t, pool, ctx, query, args)
		t.Logf("plan with a shared index:\n%s", plan)

		// The rows that do come back are still correctly filtered. The
		// leak is in the count and the latency, never in the contents.
		results, err := pool.VectorSearch(ctx, probeVector(), source,
			leakWantedRows, nil, nil)
		if err != nil {
			t.Fatalf("VectorSearch failed: %v", err)
		}
		for _, r := range results {
			if r.Content == "" {
				t.Error("result came back with empty content")
			}
		}

		if !usesIndexScan(plan) {
			// The planner costed the approximate index out of the query
			// for this corpus. That is not a failure of the mitigation and
			// not a contradiction of the exposure: the side channel exists
			// only when the shared index is the thing answering the query.
			// Reported rather than asserted, so this test never fails for
			// a reason that has nothing to do with the code under test.
			t.Logf("the planner did not choose the approximate index for this "+
				"corpus (%d rows for the minority identity of %d total), so the "+
				"shortfall cannot arise here; the exposure this test documents "+
				"needs the index scan to be the thing answering the query",
				leakMinorityRows, leakMajorityRows+leakMinorityRows)
			return
		}

		if delivered >= leakWantedRows {
			t.Fatalf("the shared HNSW index answered the query but delivered all "+
				"%d requested rows for an identity owning %d of %d; the side "+
				"channel this test documents did not reproduce, which is worth "+
				"understanding before this assertion is relaxed",
				leakWantedRows, leakMinorityRows,
				leakMajorityRows+leakMinorityRows)
		}

		t.Logf("shared index: caller asked for %d rows and received %d — the "+
			"shortfall is a function of the other identity's corpus density "+
			"near the query vector, which is what makes it a channel",
			leakWantedRows, delivered)
	})

	t.Run("default configuration returns the full result set", func(t *testing.T) {
		// AllowSharedVectorIndex left at its zero value, which is the
		// default a deployment gets.
		pool := servicePool(t, role, password,
			config.IdentityConfig{Enabled: true}, 2)

		results, err := pool.VectorSearch(ctx, probeVector(), source,
			leakWantedRows, nil, nil)
		if err != nil {
			t.Fatalf("VectorSearch failed: %v", err)
		}

		if len(results) != leakWantedRows {
			t.Fatalf("with the default (exact) configuration the caller asked "+
				"for %d rows and received %d; the exact scan should return all "+
				"of them", leakWantedRows, len(results))
		}
	})
}

// TestExactSearchDisablesIndexScans checks the mechanism behind the
// default directly, rather than only through its effect: the
// identity-bearing transaction turns index and bitmap scans off, and
// turns them back on when a deployment opts into the shared index.
//
// Asserting the planner settings as well as the row counts matters
// because the row-count assertion above would also pass if pgvector
// simply got better at filtered search. This one fails if the mechanism
// stops being applied at all.
func TestExactSearchDisablesIndexScans(t *testing.T) {
	admin := adminPool(t)
	role, password := serviceRole(t, admin)

	cases := []struct {
		name            string
		allowSharedIdx  bool
		wantIndexScanOn string
	}{
		{"default forces exact scans", false, "off"},
		{"opting in leaves the planner alone", true, "on"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := servicePool(t, role, password, config.IdentityConfig{
				Enabled:                true,
				AllowSharedVectorIndex: tc.allowSharedIdx,
			}, 1)

			var indexScan, bitmapScan string
			err := pool.withRows(callerContext(aliceClaims),
				"SELECT current_setting('enable_indexscan'), "+
					"current_setting('enable_bitmapscan')", nil,
				func(rows pgx.Rows) error {
					for rows.Next() {
						if err := rows.Scan(&indexScan, &bitmapScan); err != nil {
							return err
						}
					}
					return rows.Err()
				})
			if err != nil {
				t.Fatalf("probe query failed: %v", err)
			}

			if indexScan != tc.wantIndexScanOn || bitmapScan != tc.wantIndexScanOn {
				t.Errorf("enable_indexscan=%q enable_bitmapscan=%q, want both %q",
					indexScan, bitmapScan, tc.wantIndexScanOn)
			}

			// And the setting must not outlive the transaction either.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			conn, err := pool.pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("failed to acquire the pooled connection: %v", err)
			}
			var after string
			err = conn.QueryRow(ctx, "SELECT current_setting('enable_indexscan')").
				Scan(&after)
			conn.Release()
			if err != nil {
				t.Fatalf("failed to inspect the pooled connection: %v", err)
			}
			if after != "on" {
				t.Errorf("enable_indexscan is %q on the pooled connection after "+
					"the request, want the session default \"on\"", after)
			}
		})
	}
}
