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
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

// TestDatabaseURLEnv names the environment variable holding a
// connection string for a PostgreSQL instance the identity tests may
// create and drop objects in. It must be a superuser connection: the
// tests create login roles, since the whole point is to observe what
// row-level security does to a role that is not the table owner, and
// that cannot be arranged from inside a single unprivileged session.
//
// Tests that need it skip when it is unset, so `go test ./...` still
// works on a machine with no database. CI sets it — see
// .github/workflows/ci.yml. A skip here is not a pass: these are the
// only tests in the repository that can observe an identity leak.
const TestDatabaseURLEnv = "RAG_TEST_DATABASE_URL"

// adminPool returns a pool connected with the credentials in
// TestDatabaseURLEnv, skipping the test if it is unset.
func adminPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv(TestDatabaseURLEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping database-backed identity test",
			TestDatabaseURLEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to %s: %v", TestDatabaseURLEnv, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping %s: %v", TestDatabaseURLEnv, err)
	}

	t.Cleanup(pool.Close)
	return pool
}

// uniqueName returns an identifier unique to this test, so that
// concurrent packages or a re-run after a crash do not collide on
// cluster-wide objects such as roles.
func uniqueName(t *testing.T, prefix string) string {
	t.Helper()

	// t.Name() is unique within a run; the process id separates runs.
	// Both are folded to the character class PostgreSQL accepts
	// unquoted, so the result never needs quoting.
	sanitized := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, t.Name())

	// PostgreSQL truncates identifiers at NAMEDATALEN (63 bytes by
	// default). Truncating the test-name portion here rather than
	// letting the server do it keeps the name this function returns and
	// the name the server stores identical, so cleanup finds what setup
	// created.
	const maxTestPart = 24
	if len(sanitized) > maxTestPart {
		sanitized = sanitized[:maxTestPart]
	}

	return fmt.Sprintf("%s_%s_%d", prefix, sanitized, os.Getpid())
}

// exec runs a statement on the admin pool and fails the test if it
// errors. Test fixture DDL only; nothing here handles caller input.
func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("fixture statement failed: %v\nSQL: %s", err, sql)
	}
}

// serviceRole creates a login role for a pool to connect as, and
// registers its removal.
//
// The tests connect as a role that neither owns the fixture tables nor
// has BYPASSRLS, because that is the only arrangement in which
// row-level security actually runs. A test that connected as the owner
// would see every row whatever identity it presented, and would
// therefore pass against the very bug it is meant to catch.
func serviceRole(t *testing.T, pool *pgxpool.Pool) (name, password string) {
	t.Helper()

	name = uniqueName(t, "rag_svc")
	password = "test_" + name

	exec(t, pool, fmt.Sprintf("DROP ROLE IF EXISTS %s",
		pgx.Identifier{name}.Sanitize()))
	exec(t, pool, fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD %s",
		pgx.Identifier{name}.Sanitize(), quoteLiteral(password)))

	t.Cleanup(func() { dropRole(t, pool, name) })

	return name, password
}

// dropRole removes a role created by a fixture, and reports a failure
// to do so rather than swallowing it.
//
// The reassignment is not belt and braces. A role that owns a table
// cannot be dropped, and t.Cleanup runs last-registered-first, so a role
// created after the schema it goes on to own is dropped while that table
// still exists: the DROP fails, and if the error is discarded the role
// accumulates in the test database, one per run. That is exactly what
// happened before this helper existed — four rag_claimable_* roles left
// behind by the ownership subtests, found only because the discarded
// error was looked for.
//
// REASSIGN OWNED BY hands anything the role owns to the current user and
// DROP OWNED BY clears the privileges granted to it, so the role can
// then go regardless of what a fixture gave it or of the order cleanups
// happen to run in.
func dropRole(t *testing.T, pool *pgxpool.Pool, name string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	quoted := pgx.Identifier{name}.Sanitize()
	for _, stmt := range []string{
		fmt.Sprintf("REASSIGN OWNED BY %s TO CURRENT_USER", quoted),
		fmt.Sprintf("DROP OWNED BY %s", quoted),
		fmt.Sprintf("DROP ROLE IF EXISTS %s", quoted),
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Errorf("failed to clean up role %s: %v\nSQL: %s", name, err, stmt)
			return
		}
	}
}

// quoteLiteral quotes a string for use as an SQL literal in fixture
// DDL, where a bound parameter is not available (CREATE ROLE ...
// PASSWORD does not accept one).
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// testSchema creates a schema for one test's fixtures and registers its
// removal.
func testSchema(t *testing.T, pool *pgxpool.Pool, role string) string {
	t.Helper()

	schema := uniqueName(t, "rag_test")
	quoted := pgx.Identifier{schema}.Sanitize()

	exec(t, pool, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", quoted))
	exec(t, pool, fmt.Sprintf("CREATE SCHEMA %s", quoted))
	exec(t, pool, fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s",
		quoted, pgx.Identifier{role}.Sanitize()))

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", quoted))
	})

	return schema
}

// servicePool builds a *Pool connected as the given role, with the
// given identity configuration and connection limit.
//
// It assembles the Pool struct directly rather than going through
// NewPool because two of the tests need control NewPool does not
// expose: which role to connect as (NewPool takes a DatabaseConfig, not
// a DSN) and how many connections the pool may open. maxConns=1 is what
// makes the pooled-connection test a real test rather than a hopeful
// one — with a single connection, the second request provably lands on
// the same backend as the first.
func servicePool(
	t *testing.T,
	role, password string,
	idCfg config.IdentityConfig,
	maxConns int32,
) *Pool {
	t.Helper()

	dsn := os.Getenv(TestDatabaseURLEnv)
	if dsn == "" {
		t.Skipf("%s is not set", TestDatabaseURLEnv)
	}

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", TestDatabaseURLEnv, err)
	}
	poolCfg.ConnConfig.User = role
	poolCfg.ConnConfig.Password = password
	poolCfg.MaxConns = maxConns

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pgxPool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("failed to create pool as %s: %v", role, err)
	}
	if err := pgxPool.Ping(ctx); err != nil {
		pgxPool.Close()
		t.Fatalf("failed to connect as %s: %v", role, err)
	}

	t.Cleanup(pgxPool.Close)

	return &Pool{pool: pgxPool, identity: idCfg.WithDefaults()}
}

// enabledIdentity returns an identity configuration with identity on
// and the enforcement preflight left at its default.
func enabledIdentity() config.IdentityConfig {
	return config.IdentityConfig{Enabled: true}.WithDefaults()
}

// hasPGVector reports whether the pgvector extension is available and
// installed, installing it if the admin role may.
func hasPGVector(t *testing.T, pool *pgxpool.Pool) bool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		t.Logf("pgvector is not available (%v)", err)
		return false
	}
	return true
}
