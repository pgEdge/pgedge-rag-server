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
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
)

// FailureKind classifies why a retrieval attempt failed, so that a
// caller can be told something true and useful about a failed search
// without being shown the underlying error — see issue #49.
//
// The distinction that matters is what the person on the other end can
// do about it. A refused query is deterministic and needs a
// configuration or grant change by whoever deployed the pipeline;
// retrying it will fail identically. An unreachable database is
// transient and may well succeed on the next attempt. Reporting both as
// "an internal error occurred" points nobody at anything.
//
// The constants are ordered by precedence, not severity: when several
// tables fail in different ways, the highest kind wins. Refused
// outranks Unreachable because a refusal proves the database answered,
// so it is the finding that leaves the operator with work to do.
type FailureKind int

const (
	// FailureNone means no failure — a nil error.
	FailureNone FailureKind = iota

	// FailureUnknown is a failure that could not be classified. It is
	// still a failure, and still must not be reported as an empty
	// result set.
	FailureUnknown

	// FailureUnreachable means the search never ran because the
	// database could not be reached, or refused to serve connections.
	FailureUnreachable

	// FailureRefused means the database received the query and refused
	// it: insufficient privilege, a missing table or column, an
	// unavailable operator or extension. Every case is a configuration
	// or permissions problem on the server side.
	FailureRefused
)

// String renders the kind for logging. These strings are for the
// operator's log only; the client-facing text lives in the safeerr
// package, which deliberately says less.
func (k FailureKind) String() string {
	switch k {
	case FailureNone:
		return "none"
	case FailureRefused:
		return "refused"
	case FailureUnreachable:
		return "unreachable"
	default:
		return "unknown"
	}
}

// refusedSQLStateClasses are the SQLSTATE classes that mean "the
// database understood the request and would not run it", each of which
// is fixed by changing configuration or grants rather than by retrying:
//
//	28 — invalid authorization specification (bad database credentials)
//	3D — invalid catalog name (the configured database does not exist)
//	3F — invalid schema name
//	42 — syntax error or access rule violation. This is the class that
//	     carries 42501 insufficient_privilege, 42P01 undefined_table,
//	     42703 undefined_column and 42883 undefined_function — the last
//	     being what a missing pgvector extension looks like, since the
//	     <=> operator then does not resolve.
//
// Classification is by class rather than by individual code because the
// response is the same for the whole class and an unlisted member of it
// would otherwise fall through to "unknown".
var refusedSQLStateClasses = map[string]bool{
	"28": true,
	"3D": true,
	"3F": true,
	"42": true,
}

// unreachableSQLStateClasses are the SQLSTATE classes that mean the
// database is not in a position to serve the query at all:
//
//	08 — connection exception
//	53 — insufficient resources (53300 too_many_connections)
//	57 — operator intervention (57P01 admin_shutdown,
//	     57P03 cannot_connect_now)
//
// These arrive as a PgError rather than a transport error because the
// server was reached and said no; from the caller's point of view they
// are nonetheless "the database is down", and retrying is reasonable.
var unreachableSQLStateClasses = map[string]bool{
	"08": true,
	"53": true,
	"57": true,
}

// ClassifyFailure determines why a retrieval failed.
//
// Anything not positively recognised is FailureUnknown rather than
// being guessed at. Unknown still reports as a failure to the caller —
// the point of this function is to say more when it can, never to
// downgrade a failure into a successful empty search.
//
// Context cancellation and deadlines are deliberately not classified
// here: the server already distinguishes its own request timeout and
// answers 504 before reaching this path.
func ClassifyFailure(err error) FailureKind {
	if err == nil {
		return FailureNone
	}

	// A cancelled or timed-out request is neither a refusal nor an
	// outage, and the handler has a dedicated answer for it.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return FailureUnknown
	}

	// A PgError means the server answered, so its SQLSTATE is the most
	// authoritative signal available and is checked first.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && len(pgErr.Code) >= 2 {
		class := pgErr.Code[:2]
		switch {
		case refusedSQLStateClasses[class]:
			return FailureRefused
		case unreachableSQLStateClasses[class]:
			return FailureUnreachable
		}
		// Some other SQLSTATE: the query ran and failed for a reason
		// we have not characterised. Report it as unknown rather than
		// mislabelling it.
		return FailureUnknown
	}

	// No SQLSTATE: the failure happened below the protocol, on the way
	// to the server or in the pool.
	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) {
		return FailureUnreachable
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return FailureUnreachable
	}
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) {
		return FailureUnreachable
	}

	return FailureUnknown
}

// RetrievalError reports that a search could not be completed, carrying
// the classification the API layer needs to choose a status code and a
// client-safe message.
//
// The wrapped error keeps the full detail — SQLSTATE, table name, the
// database's own message — for the operator's log. It must never be
// rendered to an API client: use safeerr.Message, which derives its text
// from Kind alone and so cannot leak a schema detail regardless of what
// the database put in its message.
type RetrievalError struct {
	Kind FailureKind
	Err  error
}

func (e *RetrievalError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("retrieval failed (%s)", e.Kind)
	}
	return fmt.Sprintf("retrieval failed (%s): %v", e.Kind, e.Err)
}

func (e *RetrievalError) Unwrap() error {
	return e.Err
}
