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

	"github.com/jackc/pgx/v5"

	"github.com/pgEdge/pgedge-rag-server/internal/identity"
)

// noRole is the value written to the role parameter when a request does
// not assume one. PostgreSQL spells "go back to the session user" as
// NONE; an empty string is rejected outright, so this cannot be
// collapsed into "".
const noRole = "none"

// applyIdentitySQL applies a request's identity, and the planner
// settings that go with it, to the current transaction.
//
// Everything is done through set_config(name, value, is_local => true)
// with bound parameters, in one statement, for three reasons:
//
//   - is_local => true gives SET LOCAL semantics, so every value is
//     discarded when the transaction ends. That is what keeps an
//     identity from surviving onto the next request that acquires this
//     pooled connection.
//   - The claim set is a bound parameter, never interpolated into SQL
//     text. Claims originate outside this process; building a SET
//     statement out of them by string formatting would make the claim
//     set an injection vector into the very statement that establishes
//     the security context.
//   - set_config returns the value it actually installed, so a single
//     round trip both sets and confirms. See applyIdentity for why
//     confirming matters.
//
// The role is applied in the same statement rather than afterwards.
// set_config evaluates its arguments left to right, so the claim set is
// installed while still connected as the service role, and the role
// switch that may reduce privileges happens last.
const applyIdentitySQL = `SELECT
	set_config($1, $2, true),
	set_config('role', $3, true),
	set_config('enable_indexscan', $4, true),
	set_config('enable_bitmapscan', $4, true)`

// applyIdentity establishes id as the identity of tx.
//
// The values set_config reports back are compared against what was
// asked for. This is not defensive padding: a deployment can pin
// identity inside the database — an event trigger, a modified
// search_path, or a policy that resolves identity from a table keyed on
// session_user and ignores the claims parameter entirely — and in that
// arrangement the claims this server sets are accepted without error
// and then disregarded. Comparing the readback catches the subset of
// that where the parameter itself does not hold the value; the rest is
// caught at startup by VerifyEnforcement, because it cannot be seen
// from here at all.
func applyIdentity(
	ctx context.Context,
	tx pgx.Tx,
	setting string,
	id *identity.Identity,
	exactSearch bool,
) error {
	role := noRole
	if id.Role != "" {
		role = id.Role
	}

	indexScans := "on"
	if exactSearch {
		indexScans = "off"
	}

	var gotClaims, gotRole, gotIndexScan, gotBitmapScan string
	err := tx.QueryRow(ctx, applyIdentitySQL, setting, id.Claims, role, indexScans).
		Scan(&gotClaims, &gotRole, &gotIndexScan, &gotBitmapScan)
	if err != nil {
		return fmt.Errorf("failed to apply request identity: %w", err)
	}

	if gotClaims != id.Claims {
		return fmt.Errorf(
			"database did not retain the request identity: %s was set but reads back "+
				"as %q; the database may be pinning identity independently of this "+
				"server, in which case row-level security is not evaluating the caller",
			setting, gotClaims)
	}

	if gotRole != role {
		return fmt.Errorf(
			"database did not retain the requested role: asked for %q, reads back as %q",
			role, gotRole)
	}

	if gotIndexScan != indexScans || gotBitmapScan != indexScans {
		return fmt.Errorf(
			"database did not retain the scan settings: asked for %q, reads back as "+
				"enable_indexscan=%q enable_bitmapscan=%q",
			indexScans, gotIndexScan, gotBitmapScan)
	}

	return nil
}

// withRows runs one query and hands its rows to scan.
//
// When identity is disabled the query goes straight to the pool, which
// is exactly what this package did before per-request identity existed.
// Enabling identity is what introduces the transaction, so a deployment
// that has not opted in pays nothing for it.
//
// When identity is enabled the query runs inside a read-only
// transaction that establishes the caller's identity and is then always
// rolled back. Rolled back rather than committed because retrieval
// never writes, and because rollback is the shortest path to discarding
// every SET LOCAL: there is no state left for the next request on this
// connection to observe, whatever happens in between.
//
// scan must consume rows fully before returning; the transaction is
// rolled back and the connection released as soon as it does.
func (p *Pool) withRows(
	ctx context.Context,
	sql string,
	args []interface{},
	scan func(pgx.Rows) error,
) error {
	if !p.identity.Enabled {
		rows, err := p.pool.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		return scan(rows)
	}

	// The backstop for the HTTP layer. The middleware refuses an
	// unidentified request before any work is done, but that is a
	// policy check in one place; this is the structural one. No query
	// reaches a table without an identity, however it was dispatched,
	// and in particular a future endpoint that forgets the middleware
	// fails closed rather than quietly running as the service role.
	id, ok := identity.FromContext(ctx)
	if !ok {
		return identity.ErrRequired
	}

	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	// context.WithoutCancel so that a cancelled or timed-out request
	// still issues its rollback. Without it, a client disconnecting
	// mid-query would leave the connection inside an aborted
	// transaction; pgxpool destroys such a connection rather than
	// reusing it, so identity would not leak either way, but the pool
	// would churn connections under exactly the conditions where it can
	// least afford to.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := applyIdentity(ctx, tx, p.identity.ClaimsSetting, id,
		!p.identity.AllowSharedVectorIndex); err != nil {
		return err
	}

	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	return scan(rows)
}
