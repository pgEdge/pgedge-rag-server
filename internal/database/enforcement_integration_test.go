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
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

// enforcementFixture describes one arrangement of a corpus table and
// what VerifyEnforcement should make of it.
type enforcementFixture struct {
	name string

	// setup creates the table in the given schema, owned and granted so
	// as to produce the arrangement under test. It returns the table
	// name relative to the schema.
	setup func(t *testing.T, admin *pgxpool.Pool, schema, role string) string

	// wantProblem is empty when enforcement should verify cleanly.
	// Otherwise it is a substring the reported problem must contain, so
	// the test pins down which problem was detected rather than merely
	// that something was.
	wantProblem string
}

// TestVerifyEnforcement covers the startup preflight against every
// arrangement it is meant to catch.
//
// The last of these is the one the preflight exists for. A deployment
// that pins identity inside the database — resolving a fixed identity
// from a table keyed on session_user, deliberately ignoring the claims
// parameter so a service which executes caller-supplied SQL cannot forge
// one — accepts everything this server sets and then disregards it. The
// query succeeds. Rows come back. Row-level security evaluates as the
// pinned identity for every caller. Nothing at request time can tell,
// which is why it has to be caught here.
func TestVerifyEnforcement(t *testing.T) {
	fixtures := []enforcementFixture{
		{
			name: "claims policy on a non-owned table verifies",
			setup: func(t *testing.T, admin *pgxpool.Pool, schema, role string) string {
				tenantCorpus(t, admin, schema, role, config.DefaultClaimsSetting, false)
				return "chunks"
			},
		},
		{
			name: "row-level security disabled",
			setup: func(t *testing.T, admin *pgxpool.Pool, schema, role string) string {
				q := pgx.Identifier{schema, "plain"}.Sanitize()
				exec(t, admin, fmt.Sprintf(
					"CREATE TABLE %s (id text, owner text, content text)", q))
				exec(t, admin, fmt.Sprintf("GRANT SELECT ON %s TO %s",
					q, pgx.Identifier{role}.Sanitize()))
				return "plain"
			},
			wantProblem: "row-level security is not enabled",
		},
		{
			name: "row-level security enabled but no policy defined",
			setup: func(t *testing.T, admin *pgxpool.Pool, schema, role string) string {
				q := pgx.Identifier{schema, "nopolicy"}.Sanitize()
				exec(t, admin, fmt.Sprintf(
					"CREATE TABLE %s (id text, owner text, content text)", q))
				exec(t, admin, fmt.Sprintf(
					"ALTER TABLE %s ENABLE ROW LEVEL SECURITY", q))
				exec(t, admin, fmt.Sprintf("GRANT SELECT ON %s TO %s",
					q, pgx.Identifier{role}.Sanitize()))
				return "nopolicy"
			},
			wantProblem: "no policy is defined",
		},
		{
			name: "service role owns the table and it is not forced",
			setup: func(t *testing.T, admin *pgxpool.Pool, schema, role string) string {
				q := pgx.Identifier{schema, "owned"}.Sanitize()
				exec(t, admin, fmt.Sprintf(
					"CREATE TABLE %s (id text, owner text, content text)", q))
				exec(t, admin, fmt.Sprintf(
					"ALTER TABLE %s ENABLE ROW LEVEL SECURITY", q))
				exec(t, admin, fmt.Sprintf(
					`CREATE POLICY own_rows ON %s FOR SELECT
					   USING (owner = current_setting(%s, true)::json->>'sub')`,
					q, quoteLiteral(config.DefaultClaimsSetting)))
				exec(t, admin, fmt.Sprintf("ALTER TABLE %s OWNER TO %s",
					q, pgx.Identifier{role}.Sanitize()))
				return "owned"
			},
			wantProblem: "FORCE ROW LEVEL SECURITY",
		},
		{
			name: "service role owns the table and it is forced",
			setup: func(t *testing.T, admin *pgxpool.Pool, schema, role string) string {
				q := pgx.Identifier{schema, "forced"}.Sanitize()
				exec(t, admin, fmt.Sprintf(
					"CREATE TABLE %s (id text, owner text, content text)", q))
				exec(t, admin, fmt.Sprintf(
					"ALTER TABLE %s ENABLE ROW LEVEL SECURITY", q))
				exec(t, admin, fmt.Sprintf(
					"ALTER TABLE %s FORCE ROW LEVEL SECURITY", q))
				exec(t, admin, fmt.Sprintf(
					`CREATE POLICY own_rows ON %s FOR SELECT
					   USING (owner = current_setting(%s, true)::json->>'sub')`,
					q, quoteLiteral(config.DefaultClaimsSetting)))
				exec(t, admin, fmt.Sprintf("ALTER TABLE %s OWNER TO %s",
					q, pgx.Identifier{role}.Sanitize()))
				return "forced"
			},
		},
		{
			name: "configured relation does not exist",
			setup: func(t *testing.T, admin *pgxpool.Pool, schema, role string) string {
				return "absent"
			},
			wantProblem: "was not found",
		},
		{
			name: "relation is a view",
			setup: func(t *testing.T, admin *pgxpool.Pool, schema, role string) string {
				tenantCorpus(t, admin, schema, role, config.DefaultClaimsSetting, false)
				v := pgx.Identifier{schema, "chunk_view"}.Sanitize()
				exec(t, admin, fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM %s",
					v, pgx.Identifier{schema, "chunks"}.Sanitize()))
				exec(t, admin, fmt.Sprintf("GRANT SELECT ON %s TO %s",
					v, pgx.Identifier{role}.Sanitize()))
				return "chunk_view"
			},
			wantProblem: "is a view",
		},
		{
			name: "policies pin identity on session_user and ignore the claims",
			setup: func(t *testing.T, admin *pgxpool.Pool, schema, role string) string {
				// The pattern in the wild: a mapping table resolves a
				// fixed identity for a service login role, so that a
				// service which can run caller-supplied SQL cannot forge
				// one by setting request.jwt.claims itself.
				pins := pgx.Identifier{schema, "service_identity"}.Sanitize()
				exec(t, admin, fmt.Sprintf(
					"CREATE TABLE %s (login_role text PRIMARY KEY, tenant text NOT NULL)",
					pins))
				exec(t, admin, fmt.Sprintf(
					"INSERT INTO %s (login_role, tenant) VALUES ($1, 'alice')", pins),
					role)
				exec(t, admin, fmt.Sprintf("GRANT SELECT ON %s TO %s",
					pins, pgx.Identifier{role}.Sanitize()))

				q := pgx.Identifier{schema, "pinned"}.Sanitize()
				exec(t, admin, fmt.Sprintf(
					"CREATE TABLE %s (id text, owner text, content text)", q))
				exec(t, admin, fmt.Sprintf(
					"ALTER TABLE %s ENABLE ROW LEVEL SECURITY", q))
				exec(t, admin, fmt.Sprintf(
					`CREATE POLICY pinned_rows ON %s FOR SELECT
					   USING (owner = (SELECT tenant FROM %s
					                    WHERE login_role = session_user))`, q, pins))
				exec(t, admin, fmt.Sprintf("GRANT SELECT ON %s TO %s",
					q, pgx.Identifier{role}.Sanitize()))
				return "pinned"
			},
			wantProblem: "session_user",
		},
	}

	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			admin := adminPool(t)
			role, password := serviceRole(t, admin)
			schema := testSchema(t, admin, role)

			relation := fx.setup(t, admin, schema, role)

			pool := servicePool(t, role, password, enabledIdentity(), 2)
			tables := []config.TableSource{{Table: schema + "." + relation}}

			problems := pool.VerifyEnforcement(context.Background(), tables)

			if fx.wantProblem == "" {
				if len(problems) != 0 {
					t.Fatalf("expected enforcement to verify cleanly, got: %v", problems)
				}
				return
			}

			if len(problems) == 0 {
				t.Fatalf("expected a problem containing %q, but enforcement "+
					"verified cleanly", fx.wantProblem)
			}

			joined := errors.Join(problems...).Error()
			if !strings.Contains(joined, fx.wantProblem) {
				t.Errorf("problem %q does not mention %q", joined, fx.wantProblem)
			}
			for _, p := range problems {
				if !errors.Is(p, ErrEnforcementUnverified) {
					t.Errorf("problem %v does not wrap ErrEnforcementUnverified", p)
				}
			}
		})
	}
}

// TestVerifyEnforcement_BypassRLSRole checks the case where the
// connecting role can bypass row-level security outright. Kept separate
// because it alters the role rather than the table.
func TestVerifyEnforcement_BypassRLSRole(t *testing.T) {
	admin := adminPool(t)
	role, password := serviceRole(t, admin)
	schema := testSchema(t, admin, role)

	tenantCorpus(t, admin, schema, role, config.DefaultClaimsSetting, false)
	exec(t, admin, fmt.Sprintf("ALTER ROLE %s BYPASSRLS",
		pgx.Identifier{role}.Sanitize()))

	pool := servicePool(t, role, password, enabledIdentity(), 2)
	tables := []config.TableSource{{Table: schema + ".chunks"}}

	problems := pool.VerifyEnforcement(context.Background(), tables)
	if len(problems) == 0 {
		t.Fatal("a BYPASSRLS service role verified cleanly; it should not")
	}
	if !strings.Contains(errors.Join(problems...).Error(), "BYPASSRLS") {
		t.Errorf("problem does not mention BYPASSRLS: %v", problems)
	}
}

// TestVerifyEnforcement_AllowedRolesThatBypassRLS covers the roles a
// request can be switched into, which the per-table check never sees.
//
// The per-table check reads the attributes of the role the pool logs in
// as. A caller whose claims name a role on the allowlist leaves that
// role behind for the query, so a superuser or BYPASSRLS role on the
// allowlist skips every policy on every table for that caller — while
// the per-table check happily reports each table as verified. Both ways
// of bypassing are covered, because they are separate role attributes
// and only one of them is named BYPASSRLS.
func TestVerifyEnforcement_AllowedRolesThatBypassRLS(t *testing.T) {
	cases := []struct {
		name      string
		attribute string
		wantFlag  bool
	}{
		{"a BYPASSRLS role on the allowlist", "BYPASSRLS", true},
		{"a superuser on the allowlist", "SUPERUSER", true},
		{"an ordinary role on the allowlist", "NOSUPERUSER", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			admin := adminPool(t)
			role, password := serviceRole(t, admin)
			schema := testSchema(t, admin, role)
			tenantCorpus(t, admin, schema, role, config.DefaultClaimsSetting, false)

			claimable := uniqueName(t, "rag_claimable")
			exec(t, admin, fmt.Sprintf("DROP ROLE IF EXISTS %s",
				pgx.Identifier{claimable}.Sanitize()))
			exec(t, admin, fmt.Sprintf("CREATE ROLE %s %s",
				pgx.Identifier{claimable}.Sanitize(), tc.attribute))
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_, _ = admin.Exec(ctx, fmt.Sprintf("DROP ROLE IF EXISTS %s",
					pgx.Identifier{claimable}.Sanitize()))
			})

			idCfg := config.IdentityConfig{
				Enabled:      true,
				AllowedRoles: []string{claimable},
			}.WithDefaults()

			pool := servicePool(t, role, password, idCfg, 2)
			tables := []config.TableSource{{Table: schema + ".chunks"}}

			problems := pool.VerifyEnforcement(context.Background(), tables)

			if !tc.wantFlag {
				if len(problems) != 0 {
					t.Fatalf("an ordinary allowlisted role was flagged: %v", problems)
				}
				return
			}

			if len(problems) == 0 {
				t.Fatalf("a %s role on identity.allowed_roles verified cleanly; "+
					"a caller claiming it would see every row of every table",
					tc.attribute)
			}

			joined := errors.Join(problems...).Error()
			if !strings.Contains(joined, claimable) {
				t.Errorf("the problem does not name the offending role %q: %s",
					claimable, joined)
			}
			if !strings.Contains(joined, "allowed_roles") {
				t.Errorf("the problem does not point at identity.allowed_roles: %s",
					joined)
			}
		})
	}
}

// TestVerifyEnforcement_SkippedWhenNotApplicable checks the two ways the
// preflight is meant not to run: identity disabled, and the check
// explicitly turned off. Both use a table that would certainly fail it.
func TestVerifyEnforcement_SkippedWhenNotApplicable(t *testing.T) {
	admin := adminPool(t)
	role, password := serviceRole(t, admin)
	schema := testSchema(t, admin, role)

	q := pgx.Identifier{schema, "wide_open"}.Sanitize()
	exec(t, admin, fmt.Sprintf("CREATE TABLE %s (id text, content text)", q))
	exec(t, admin, fmt.Sprintf("GRANT SELECT ON %s TO %s",
		q, pgx.Identifier{role}.Sanitize()))

	tables := []config.TableSource{{Table: schema + ".wide_open"}}

	cases := []struct {
		name string
		cfg  config.IdentityConfig
	}{
		{"identity disabled", config.IdentityConfig{Enabled: false}},
		{"check turned off", config.IdentityConfig{
			Enabled:          true,
			EnforcementCheck: config.EnforcementCheckOff,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := servicePool(t, role, password, tc.cfg, 2)
			if problems := pool.VerifyEnforcement(context.Background(), tables); len(problems) != 0 {
				t.Errorf("expected the preflight not to run, got: %v", problems)
			}
		})
	}
}
