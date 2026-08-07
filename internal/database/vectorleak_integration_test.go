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
	"github.com/pgEdge/pgedge-rag-server/internal/identity"
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

// explainAnalyze runs a query under EXPLAIN ANALYZE in an
// identity-bearing transaction, and returns the plan text together with
// the number of rows the query actually produced.
//
// Plan and row count come from the same execution deliberately. Reading
// the plan with one statement and the row count with another looks
// equivalent but is not: PostgreSQL may plan a parameterised query
// differently between a custom and a generic plan, so the two
// statements can disagree about whether the index was used — and an
// assertion that pairs "the plan says index scan" with "this other
// query returned n rows" is then asserting nothing in particular. One
// statement cannot disagree with itself.
//
// forceIndexScan disables sequential scans for the query, which is how
// the side-channel demonstration gets the approximate index to answer
// the query on any PostgreSQL version. Without it the test is at the
// mercy of a cost estimate: on PostgreSQL 16 with pgvector 0.6 the
// planner picks the HNSW index for this corpus and the shortfall
// appears, while on PostgreSQL 17 with pgvector 0.8 it picks a
// sequential scan and the demonstration quietly does not happen. The
// exposure is a property of the index answering the query, not of the
// planner choosing to let it, so the test makes that condition hold
// rather than hoping for it.
//
// This assembles the transaction itself rather than going through
// withRows, because withRows runs exactly one statement and the planner
// setting has to be established alongside the identity. It calls the
// production applyIdentity so the session state under test is the same
// state a real query would run with.
func explainAnalyze(
	ctx context.Context,
	t *testing.T,
	pool *Pool,
	query string,
	args []interface{},
	forceIndexScan bool,
) (plan string, actualRows int) {
	t.Helper()

	id, ok := identity.FromContext(ctx)
	if !ok {
		t.Fatal("explainAnalyze needs an identity in its context")
	}

	conn, err := pool.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire a connection: %v", err)
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("failed to begin a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := applyIdentity(ctx, tx, pool.identity.ClaimsSetting, id,
		pool.exactSearch(vectorQuery)); err != nil {
		t.Fatalf("failed to apply the request identity: %v", err)
	}

	if forceIndexScan {
		if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
			t.Fatalf("failed to disable sequential scans: %v", err)
		}
	}

	rows, err := tx.Query(ctx,
		"EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF, SUMMARY OFF) "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN ANALYZE failed: %v", err)
	}

	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatalf("failed to read the plan: %v", err)
		}
		lines = append(lines, line)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to read the plan: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("EXPLAIN ANALYZE returned no plan")
	}

	// The topmost node is the Limit, and its actual row count is what the
	// caller would have received.
	delivered, err := parseActualRows(lines[0])
	if err != nil {
		t.Fatalf("could not read the row count from plan line %q: %v", lines[0], err)
	}

	return strings.Join(lines, "\n"), delivered
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

// answeredByVectorIndex reports whether the plan had the approximate
// vector index produce the ordering, which is the only arrangement in
// which the side channel arises.
//
// "Index Scan" alone is not enough, and assuming it was cost a wrong
// failure: with sequential scans disabled the planner can instead read
// the table through its primary key and sort the result, which is an
// Index Scan that returns exact answers and no shortfall at all. The
// distinguishing mark is the "Order By:" annotation — PostgreSQL emits
// it only when the index itself supplies the ordering, which for this
// query means pgvector walked the graph.
func answeredByVectorIndex(plan string) bool {
	return strings.Contains(plan, "Index Scan") &&
		strings.Contains(plan, "Order By:")
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
	if testing.Short() {
		t.Skip("builds a 20,000-row HNSW corpus; skipped in short mode")
	}

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

		// Plan and row count from one execution, with the approximate
		// index forced to answer the query — see explainAnalyze for why
		// leaving that to the planner makes the demonstration silently
		// conditional on the PostgreSQL and pgvector versions in use.
		plan, delivered := explainAnalyze(ctx, t, pool, query, args, true)
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

		// Failing rather than reporting-and-returning is deliberate. An
		// earlier version of this arm logged its excuse and passed, which
		// is how it came to demonstrate nothing at all on PostgreSQL 17
		// for a while. The demonstration is the point of the subtest, so
		// being unable to run it is a failure of the subtest, not a
		// detail to note in passing. With sequential scans disabled the
		// vector index answers this on every version tried — PostgreSQL
		// 16 with pgvector 0.6 and PostgreSQL 17 with pgvector 0.8 — so
		// reaching here means something changed that a person should
		// look at.
		if !answeredByVectorIndex(plan) {
			t.Fatalf("the vector index did not supply the ordering even with "+
				"sequential scans disabled — the query was answered some other "+
				"way, such as by reading the primary key and sorting, so the "+
				"exposure this subtest exists to demonstrate cannot arise. "+
				"Plan:\n%s", plan)
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
		kind            queryKind
		allowSharedIdx  bool
		wantIndexScanOn string
	}{
		{"a vector query defaults to an exact scan", vectorQuery, false, "off"},
		{"opting in leaves the planner alone", vectorQuery, true, "on"},
		// The mitigation exists for the approximate vector index. The
		// keyword-corpus read and the fetch-by-id lookup do not consult
		// it, so disabling index scans for them would force sequential
		// scans over primary keys and ordinary filter predicates for no
		// security benefit at all.
		{"a non-vector query keeps its indexes", ordinaryQuery, false, "on"},
		{"a non-vector query keeps its indexes when opted in",
			ordinaryQuery, true, "on"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := servicePool(t, role, password, config.IdentityConfig{
				Enabled:                true,
				AllowSharedVectorIndex: tc.allowSharedIdx,
			}, 1)

			var indexScan, bitmapScan string
			err := pool.withRows(callerContext(aliceClaims), tc.kind,
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

			// And neither setting may outlive the transaction. Both are
			// read, not just the first: they are applied together, but
			// "applied together" is an assumption about the code rather
			// than an observation of it, and this is the assertion that
			// the connection carries nothing forward.
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			conn, err := pool.pool.Acquire(ctx)
			if err != nil {
				t.Fatalf("failed to acquire the pooled connection: %v", err)
			}
			var afterIndexScan, afterBitmapScan string
			err = conn.QueryRow(ctx,
				"SELECT current_setting('enable_indexscan'), "+
					"current_setting('enable_bitmapscan')").
				Scan(&afterIndexScan, &afterBitmapScan)
			conn.Release()
			if err != nil {
				t.Fatalf("failed to inspect the pooled connection: %v", err)
			}
			if afterIndexScan != "on" || afterBitmapScan != "on" {
				t.Errorf("planner settings survived on the pooled connection "+
					"after the request: enable_indexscan=%q enable_bitmapscan=%q, "+
					"want both back at the session default \"on\"",
					afterIndexScan, afterBitmapScan)
			}
		})
	}
}
