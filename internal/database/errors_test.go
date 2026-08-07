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
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// pgErr builds the error pgx returns when the server answers with an
// ERROR carrying the given SQLSTATE, wrapped the way the search code
// wraps it so the test also proves classification survives wrapping.
func pgErr(code, message string) error {
	return fmt.Errorf("vector search failed: %w", &pgconn.PgError{
		Severity: "ERROR",
		Code:     code,
		Message:  message,
	})
}

// TestClassifyFailure covers the distinction issue #49 turns on: a
// refused query is a deployment fault that will fail identically on
// retry, while an unreachable database is transient. Anything else must
// stay unknown rather than being guessed into one of those buckets.
func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want FailureKind
	}{
		{"nil", nil, FailureNone},
		{
			"insufficient privilege",
			pgErr("42501", "permission denied for table docs"),
			FailureRefused,
		},
		{
			"undefined table",
			pgErr("42P01", `relation "docs" does not exist`),
			FailureRefused,
		},
		{
			"undefined column",
			pgErr("42703", `column "embedding" does not exist`),
			FailureRefused,
		},
		{
			// A missing pgvector extension: the <=> operator does not
			// resolve, which is a configuration fault, not an outage.
			"undefined function",
			pgErr("42883", "operator does not exist: vector <=> vector"),
			FailureRefused,
		},
		{
			"invalid password",
			pgErr("28P01", "password authentication failed"),
			FailureRefused,
		},
		{
			"invalid catalog name",
			pgErr("3D000", `database "rag" does not exist`),
			FailureRefused,
		},
		{
			"invalid schema name",
			pgErr("3F000", `schema "vectors" does not exist`),
			FailureRefused,
		},
		{
			"connection exception",
			pgErr("08006", "connection failure"),
			FailureUnreachable,
		},
		{
			"too many connections",
			pgErr("53300", "too many clients already"),
			FailureUnreachable,
		},
		{
			"admin shutdown",
			pgErr("57P01", "terminating connection due to administrator command"),
			FailureUnreachable,
		},
		{
			// A SQLSTATE outside the classified sets is a genuine
			// failure of an uncharacterised kind.
			"unclassified sqlstate",
			pgErr("22000", "expected 1536 dimensions, not 768"),
			FailureUnknown,
		},
		{
			"connection refused",
			fmt.Errorf("dial: %w", syscall.ECONNREFUSED),
			FailureUnreachable,
		},
		{
			"connection reset",
			fmt.Errorf("read: %w", syscall.ECONNRESET),
			FailureUnreachable,
		},
		{
			"host unreachable",
			fmt.Errorf("dial: %w", syscall.EHOSTUNREACH),
			FailureUnreachable,
		},
		{
			"network unreachable",
			fmt.Errorf("dial: %w", syscall.ENETUNREACH),
			FailureUnreachable,
		},
		{
			"net.Error timeout",
			fmt.Errorf("dial: %w", &net.OpError{
				Op: "dial", Net: "tcp", Err: errors.New("i/o timeout"),
			}),
			FailureUnreachable,
		},
		{
			// The server's own request timeout has a dedicated 504
			// response, so this path must not claim it as an outage.
			"context deadline exceeded",
			fmt.Errorf("query: %w", context.DeadlineExceeded),
			FailureUnknown,
		},
		{
			"unrecognised error",
			errors.New("something else went wrong"),
			FailureUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyFailure(tt.err); got != tt.want {
				t.Errorf("ClassifyFailure(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestClassifyFailure_ConnectError checks a real pgconn connect-time
// failure, which carries no SQLSTATE because the server never got as
// far as answering. It uses a genuine failed connection to a closed
// local port rather than a hand-built error, since *pgconn.ConnectError
// cannot be constructed from outside pgconn — and a hand-built stand-in
// would prove nothing about the error pgx actually returns.
func TestClassifyFailure_ConnectError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Port 1 on loopback: refused immediately, no DNS, no network.
	conn, err := pgconn.Connect(ctx,
		"postgres://rag@127.0.0.1:1/rag?sslmode=disable&connect_timeout=2")
	if err == nil {
		conn.Close(ctx)
		t.Skip("something is listening on 127.0.0.1:1; cannot produce a connect failure")
	}

	var connErr *pgconn.ConnectError
	if !errors.As(err, &connErr) {
		t.Fatalf("expected a *pgconn.ConnectError from a refused connection, got %T: %v", err, err)
	}
	if got := ClassifyFailure(err); got != FailureUnreachable {
		t.Errorf("ClassifyFailure(%v) = %v, want %v", err, got, FailureUnreachable)
	}
}

// TestRetrievalError_PreservesDetailForTheLog verifies the operator's
// half of the contract: the wrapped error keeps the database's own
// message and stays reachable through errors.Is/As, so the log can carry
// the detail the client-facing message deliberately omits.
func TestRetrievalError_PreservesDetailForTheLog(t *testing.T) {
	cause := pgErr("42501", "permission denied for table docs")
	err := &RetrievalError{Kind: FailureRefused, Err: cause}

	if !errors.Is(err, cause) {
		t.Error("expected the wrapped cause to remain reachable via errors.Is")
	}

	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) {
		t.Fatal("expected the underlying *pgconn.PgError to remain reachable via errors.As")
	}
	if pgError.Code != "42501" {
		t.Errorf("expected SQLSTATE 42501 in the log detail, got %q", pgError.Code)
	}

	msg := err.Error()
	if !strings.Contains(msg, "refused") {
		t.Errorf("expected the kind in the error text for the log, got %q", msg)
	}
	if !strings.Contains(msg, "permission denied for table docs") {
		t.Errorf("expected the database's own message in the error text, got %q", msg)
	}
}

// TestRetrievalError_NilCause guards the formatting path used when a
// failure is recorded without an underlying error.
func TestRetrievalError_NilCause(t *testing.T) {
	err := &RetrievalError{Kind: FailureUnreachable}

	if got, want := err.Error(), "retrieval failed (unreachable)"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
	if err.Unwrap() != nil {
		t.Errorf("expected a nil cause, got %v", err.Unwrap())
	}
}
