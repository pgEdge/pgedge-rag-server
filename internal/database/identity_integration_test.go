//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Tests in this file run against a real PostgreSQL instance, named by
// RAG_TEST_DATABASE_URL. They are the only tests that can observe the
// behaviour this feature exists for: whether row-level security sees the
// caller's identity, whether an identity survives onto the next request
// that reuses a pooled connection, and whether an unidentified caller is
// refused rather than served.
//
// None of that can be established with a mock. A fake database that
// returns whatever rows the test told it to would pass identically
// before and after this change, which is the failure mode the tests are
// meant to rule out.

package database

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/identity"
)

// aliceClaims and bobClaims are the two callers used throughout.
const (
	aliceClaims = `{"sub":"alice"}`
	bobClaims   = `{"sub":"bob"}`
)

// callerContext returns a context carrying the given claim set.
func callerContext(claims string) context.Context {
	return identity.NewContext(context.Background(), &identity.Identity{
		Claims:  claims,
		Subject: claims,
	})
}

// tenantCorpus creates a two-tenant table with row-level security keyed
// on the claims parameter, and returns its qualified name.
//
// The table is owned by the admin role and read by the service role, so
// policies are actually evaluated. See serviceRole for why that matters.
func tenantCorpus(
	t *testing.T,
	admin *pgxpool.Pool,
	schema, role, claimsSetting string,
	withVectors bool,
) string {
	t.Helper()

	qualified := pgx.Identifier{schema, "chunks"}.Sanitize()

	vectorColumn := ""
	if withVectors {
		vectorColumn = ", embedding vector(4)"
	}

	exec(t, admin, fmt.Sprintf(`CREATE TABLE %s (
		id text PRIMARY KEY,
		owner text NOT NULL,
		content text NOT NULL%s)`, qualified, vectorColumn))

	exec(t, admin, fmt.Sprintf(
		"ALTER TABLE %s ENABLE ROW LEVEL SECURITY", qualified))
	exec(t, admin, fmt.Sprintf(
		`CREATE POLICY own_rows ON %s FOR SELECT
		   USING (owner = current_setting(%s, true)::json->>'sub')`,
		qualified, quoteLiteral(claimsSetting)))
	exec(t, admin, fmt.Sprintf("GRANT SELECT ON %s TO %s",
		qualified, pgx.Identifier{role}.Sanitize()))

	return schema + ".chunks"
}

// seedTenantRows inserts rows for both tenants.
func seedTenantRows(t *testing.T, admin *pgxpool.Pool, schema string, withVectors bool) {
	t.Helper()

	qualified := pgx.Identifier{schema, "chunks"}.Sanitize()

	rows := []struct {
		id, owner, content string
		embedding          string
	}{
		{"a1", "alice", "alice first document", "[1,0,0,0]"},
		{"a2", "alice", "alice second document", "[0.9,0.1,0,0]"},
		{"b1", "bob", "bob first document", "[0,1,0,0]"},
		{"b2", "bob", "bob second document", "[0,0.9,0.1,0]"},
	}

	for _, r := range rows {
		if withVectors {
			exec(t, admin, fmt.Sprintf(
				"INSERT INTO %s (id, owner, content, embedding) VALUES ($1,$2,$3,$4::vector)",
				qualified), r.id, r.owner, r.content, r.embedding)
			continue
		}
		exec(t, admin, fmt.Sprintf(
			"INSERT INTO %s (id, owner, content) VALUES ($1,$2,$3)",
			qualified), r.id, r.owner, r.content)
	}
}

// sortedKeys returns a document map's keys in sorted order, for stable
// comparison.
func sortedKeys(docs map[string]string) []string {
	keys := make([]string, 0, len(docs))
	for k := range docs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestIdentity_CallerSeesOnlyOwnRows is the test the whole change is
// for: two callers query the same table through the same pool and each
// gets only their own rows.
//
// Both directions are asserted, and the positive one carries the weight.
// "Alice cannot see Bob's rows" passes against the old behaviour in a
// deployment where the service role has no claims set, because the
// policy then matches nothing and everyone sees zero rows. "Alice sees
// exactly her own two rows, and Bob sees exactly his" cannot pass unless
// the identity actually reached the database session.
func TestIdentity_CallerSeesOnlyOwnRows(t *testing.T) {
	admin := adminPool(t)
	role, password := serviceRole(t, admin)
	schema := testSchema(t, admin, role)

	idCfg := enabledIdentity()
	table := tenantCorpus(t, admin, schema, role, idCfg.ClaimsSetting, false)
	seedTenantRows(t, admin, schema, false)

	pool := servicePool(t, role, password, idCfg, 4)
	source := config.TableSource{
		Table:      table,
		TextColumn: "content",
		IDColumn:   "id",
	}

	cases := []struct {
		name   string
		claims string
		want   []string
	}{
		{"alice", aliceClaims, []string{"a1", "a2"}},
		{"bob", bobClaims, []string{"b1", "b2"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			docs, err := pool.FetchDocuments(callerContext(tc.claims), source, nil, 100)
			if err != nil {
				t.Fatalf("FetchDocuments failed: %v", err)
			}

			got := sortedKeys(docs)
			if !equalStrings(got, tc.want) {
				t.Errorf("caller %s saw rows %v, want exactly %v", tc.name, got, tc.want)
			}

			for id, content := range docs {
				if content == "" {
					t.Errorf("row %s came back with empty content", id)
				}
			}
		})
	}
}

// TestIdentity_DoesNotSurviveOnPooledConnection proves that an identity
// established for one request is gone before the next request touches
// that connection.
//
// The pool is capped at one connection, so "the next request" is not a
// hope about pool behaviour — there is only one backend it can land on.
// The test confirms that by comparing pg_backend_pid() across the two
// requests: if the pids differ, the pool did something unexpected and
// the test fails rather than passing vacuously.
func TestIdentity_DoesNotSurviveOnPooledConnection(t *testing.T) {
	admin := adminPool(t)
	role, password := serviceRole(t, admin)
	schema := testSchema(t, admin, role)

	idCfg := enabledIdentity()
	table := tenantCorpus(t, admin, schema, role, idCfg.ClaimsSetting, false)
	seedTenantRows(t, admin, schema, false)

	// One connection, so every query in this test provably runs on the
	// same backend.
	pool := servicePool(t, role, password, idCfg, 1)
	source := config.TableSource{
		Table:      table,
		TextColumn: "content",
		IDColumn:   "id",
	}

	// probe runs a query through the identity-bearing path that reports
	// the backend it ran on and the claims parameter as the database saw
	// it during the query.
	probe := func(ctx context.Context) (backendPID string, claims string) {
		t.Helper()
		result := map[string]string{}
		err := pool.withRows(ctx, ordinaryQuery,
			"SELECT pg_backend_pid()::text, current_setting($1, true)",
			[]interface{}{idCfg.ClaimsSetting},
			func(rows pgx.Rows) error {
				for rows.Next() {
					var pid, seen string
					if err := rows.Scan(&pid, &seen); err != nil {
						return err
					}
					result[pid] = seen
				}
				return rows.Err()
			})
		if err != nil {
			t.Fatalf("probe query failed: %v", err)
		}
		for pid, seen := range result {
			return pid, seen
		}
		t.Fatal("probe query returned no rows")
		return "", ""
	}

	alicePID, aliceSeen := probe(callerContext(aliceClaims))
	if aliceSeen != aliceClaims {
		t.Fatalf("during alice's request the database saw claims %q, want %q",
			aliceSeen, aliceClaims)
	}

	// Now look at that same connection from outside any identity-bearing
	// transaction. This is what the *next* request would find if the
	// identity had been left behind.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := pool.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire the pooled connection: %v", err)
	}
	var residentPID, residentClaims string
	err = conn.QueryRow(ctx,
		"SELECT pg_backend_pid()::text, current_setting($1, true)",
		idCfg.ClaimsSetting).Scan(&residentPID, &residentClaims)
	conn.Release()
	if err != nil {
		t.Fatalf("failed to inspect the pooled connection: %v", err)
	}

	if residentPID != alicePID {
		t.Fatalf("the pool handed out backend %s after alice's request ran on "+
			"backend %s; this test only proves anything if the connection is "+
			"reused, so it fails rather than passing vacuously",
			residentPID, alicePID)
	}

	if residentClaims != "" {
		t.Errorf("alice's identity survived on the pooled connection: %s reads "+
			"back as %q after her request finished, want empty",
			idCfg.ClaimsSetting, residentClaims)
	}

	// And the end-to-end statement: the next caller on that same backend
	// sees her own rows, not alice's.
	bobPID, bobSeen := probe(callerContext(bobClaims))
	if bobPID != alicePID {
		t.Fatalf("bob's request ran on backend %s, alice's on %s; the pool did "+
			"not reuse the connection", bobPID, alicePID)
	}
	if bobSeen != bobClaims {
		t.Errorf("during bob's request the database saw claims %q, want %q",
			bobSeen, bobClaims)
	}

	docs, err := pool.FetchDocuments(callerContext(bobClaims), source, nil, 100)
	if err != nil {
		t.Fatalf("FetchDocuments failed: %v", err)
	}
	got := sortedKeys(docs)
	want := []string{"b1", "b2"}
	if !equalStrings(got, want) {
		t.Errorf("after alice's request on the same connection, bob saw %v, "+
			"want exactly %v", got, want)
	}
}

// TestIdentity_RoleIsAssumedAndReleased checks the PostgREST-style role
// switch: the query runs as the claimed role, and the connection is back
// to the service role afterwards.
func TestIdentity_RoleIsAssumedAndReleased(t *testing.T) {
	admin := adminPool(t)
	role, password := serviceRole(t, admin)
	tenantRole := uniqueName(t, "rag_tenant")

	exec(t, admin, fmt.Sprintf("DROP ROLE IF EXISTS %s",
		pgx.Identifier{tenantRole}.Sanitize()))
	exec(t, admin, fmt.Sprintf("CREATE ROLE %s",
		pgx.Identifier{tenantRole}.Sanitize()))
	// The service role must be a member of the role it assumes.
	exec(t, admin, fmt.Sprintf("GRANT %s TO %s",
		pgx.Identifier{tenantRole}.Sanitize(), pgx.Identifier{role}.Sanitize()))
	t.Cleanup(func() { dropRole(t, admin, tenantRole) })

	idCfg := config.IdentityConfig{
		Enabled:      true,
		AllowedRoles: []string{tenantRole},
	}.WithDefaults()

	pool := servicePool(t, role, password, idCfg, 1)

	ctx := identity.NewContext(context.Background(), &identity.Identity{
		Claims: aliceClaims,
		Role:   tenantRole,
	})

	var during string
	err := pool.withRows(ctx, ordinaryQuery, "SELECT current_user::text", nil,
		func(rows pgx.Rows) error {
			for rows.Next() {
				if err := rows.Scan(&during); err != nil {
					return err
				}
			}
			return rows.Err()
		})
	if err != nil {
		t.Fatalf("query with a role claim failed: %v", err)
	}
	if during != tenantRole {
		t.Errorf("query ran as %q, want %q", during, tenantRole)
	}

	// The role must not persist on the pooled connection either.
	acquireCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := pool.pool.Acquire(acquireCtx)
	if err != nil {
		t.Fatalf("failed to acquire the pooled connection: %v", err)
	}
	var after string
	err = conn.QueryRow(acquireCtx, "SELECT current_user::text").Scan(&after)
	conn.Release()
	if err != nil {
		t.Fatalf("failed to inspect the pooled connection: %v", err)
	}
	if after != role {
		t.Errorf("the assumed role survived on the pooled connection: "+
			"current_user is %q, want %q", after, role)
	}
}

// TestIdentity_RefusesWhenTheSettingDoesNotHold covers the readback
// guard in applyIdentity: if the database does not hold the value this
// server set, the query is refused rather than run against whatever
// identity the database decided on instead.
//
// The trigger used here is synthetic. The real case it stands in for —
// a deployment that pins identity inside the database and discards
// request.jwt.claims — cannot be staged from a test, because pinning is
// implemented in the policies rather than in the parameter, which is
// exactly why VerifyEnforcement exists as well. What can be staged is a
// run-time parameter that does not read back as it was written:
// pointing the claims setting at a boolean parameter and writing "1" to
// it produces a readback of "on".
//
// The distinction the test pins down is between "the value came back
// different" (must refuse) and "the value came back as written" (must
// proceed). That is the whole content of the guard.
func TestIdentity_RefusesWhenTheSettingDoesNotHold(t *testing.T) {
	admin := adminPool(t)
	role, password := serviceRole(t, admin)

	idCfg := config.IdentityConfig{
		Enabled:       true,
		ClaimsSetting: "enable_seqscan",
	}.WithDefaults()

	pool := servicePool(t, role, password, idCfg, 1)

	t.Run("a value that does not hold is refused", func(t *testing.T) {
		ctx := callerContext("1") // reads back as "on"

		err := pool.withRows(ctx, ordinaryQuery, "SELECT 1", nil, func(rows pgx.Rows) error {
			for rows.Next() {
			}
			return rows.Err()
		})
		if err == nil {
			t.Fatal("a claims value that the database did not retain was accepted")
		}
		if !strings.Contains(err.Error(), "did not retain the request identity") {
			t.Errorf("error does not explain what went wrong: %v", err)
		}
	})

	t.Run("a value that holds is accepted", func(t *testing.T) {
		ctx := callerContext("on")

		err := pool.withRows(ctx, ordinaryQuery, "SELECT 1", nil, func(rows pgx.Rows) error {
			for rows.Next() {
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatalf("a claims value the database did retain was refused: %v", err)
		}
	})
}

// TestIdentity_RefusesUnidentifiedCaller checks that with identity
// enabled, a query with no identity in its context is refused — and
// refused before any SQL is sent.
//
// The table named does not exist. If the guard were removed, the query
// would be dispatched and the failure would be PostgreSQL complaining
// about a missing relation, which is a different error from
// identity.ErrRequired and fails this test. That is what distinguishes
// "refused" from "attempted and happened to fail".
func TestIdentity_RefusesUnidentifiedCaller(t *testing.T) {
	admin := adminPool(t)
	role, password := serviceRole(t, admin)
	pool := servicePool(t, role, password, enabledIdentity(), 2)

	source := config.TableSource{
		Table:        "no_such_schema.no_such_table",
		TextColumn:   "content",
		VectorColumn: "embedding",
		IDColumn:     "id",
	}

	ctx := context.Background() // deliberately carries no identity

	t.Run("VectorSearch", func(t *testing.T) {
		_, err := pool.VectorSearch(ctx, []float32{1, 0, 0, 0}, source, 10, nil, nil)
		if !errors.Is(err, identity.ErrRequired) {
			t.Errorf("VectorSearch returned %v, want identity.ErrRequired", err)
		}
	})

	t.Run("FetchDocuments", func(t *testing.T) {
		_, err := pool.FetchDocuments(ctx, source, nil, 10)
		if !errors.Is(err, identity.ErrRequired) {
			t.Errorf("FetchDocuments returned %v, want identity.ErrRequired", err)
		}
	})

	t.Run("FetchDocumentsByIDs", func(t *testing.T) {
		_, err := pool.FetchDocumentsByIDs(ctx, source, []string{"a1"})
		if !errors.Is(err, identity.ErrRequired) {
			t.Errorf("FetchDocumentsByIDs returned %v, want identity.ErrRequired", err)
		}
	})
}

// TestIdentity_DisabledPoolIsUnchanged confirms the opt-in property:
// with identity disabled the pool queries exactly as it did before, with
// no identity required and no transaction wrapped around the query.
func TestIdentity_DisabledPoolIsUnchanged(t *testing.T) {
	admin := adminPool(t)
	role, password := serviceRole(t, admin)
	schema := testSchema(t, admin, role)

	table := tenantCorpus(t, admin, schema, role, config.DefaultClaimsSetting, false)
	seedTenantRows(t, admin, schema, false)

	pool := servicePool(t, role, password, config.IdentityConfig{Enabled: false}, 2)
	source := config.TableSource{
		Table:      table,
		TextColumn: "content",
		IDColumn:   "id",
	}

	// No identity in the context, and no refusal: the query runs.
	docs, err := pool.FetchDocuments(context.Background(), source, nil, 100)
	if err != nil {
		t.Fatalf("FetchDocuments with identity disabled failed: %v", err)
	}

	// The policy is keyed on a claims parameter that was never set, so
	// it matches nothing. Zero rows is the correct expectation here, and
	// it is precisely the "least privileged caller" half of the dilemma
	// this feature exists to resolve.
	if len(docs) != 0 {
		t.Errorf("with identity disabled and a claims-keyed policy in place, "+
			"the service role saw %d rows; want 0", len(docs))
	}
}
