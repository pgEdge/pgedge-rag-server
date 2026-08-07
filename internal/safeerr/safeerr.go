//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package safeerr converts internal errors into text that is safe to
// return to an API client, and scrubs credential-shaped strings from
// text that is about to be logged.
//
// The problem it solves: pgedge-go-llm-lib wraps upstream failures in
// *llm.ProviderError, whose Message field holds the provider's own error
// body verbatim and whose Error() method interpolates it. Several
// providers echo a truncated form of the submitted API key back in that
// body on an authentication failure, so relaying a raw error string to a
// caller discloses part of a real credential. This server's query and
// health endpoints are unauthenticated, so "the caller" may be anyone.
//
// The approach is to classify rather than to scrub. Message derives a
// short, fixed description from the error's type and sentinel, and never
// incorporates any upstream text, so it cannot leak a credential
// regardless of what a provider chose to put in its response body.
// Scrubbing a message that might contain a secret is guesswork;
// declining to include it is not.
//
// Redact exists for the other direction: full error detail is still
// worth logging, and Redact removes credential-shaped substrings from it
// first. That one is pattern matching and so is genuinely best-effort;
// it is defence in depth for logs, not a boundary to rely on.
package safeerr

import (
	"context"
	"errors"
	"net"
	"regexp"
	"syscall"

	llmlib "github.com/pgEdge/pgedge-go-llm-lib/llm"

	"github.com/pgEdge/pgedge-rag-server/internal/database"
)

// Client-safe descriptions. These are deliberately coarse: they tell a
// caller what class of thing went wrong without naming the provider,
// the endpoint, the model, or anything drawn from the upstream response.
const (
	MsgAuthentication = "the configured provider rejected the server's credentials"
	MsgRateLimit      = "the provider rate limit was exceeded; retry later"
	MsgInvalidRequest = "the provider rejected the request as invalid"
	MsgNotSupported   = "the requested operation is not supported by the configured provider"
	MsgProvider       = "the provider returned an error"
	MsgTimeout        = "the request took too long to process"
	MsgUnreachable    = "the provider could not be reached"
	MsgInternal       = "an internal error occurred"
)

// Client-safe descriptions of a failed document retrieval (issue #49).
//
// Each says which of three things happened — the search was refused, the
// document store could not be reached, or something else went wrong —
// and each states that no search completed, so a caller can never
// mistake one of them for an empty corpus. None of them names a table, a
// column, a SQLSTATE or any part of the query: the wording is derived
// from database.FailureKind alone, so nothing the database chose to put
// in its message can reach the caller. The detail is in the operator's
// log.
const (
	MsgRetrievalRefused = "the document search could not be run because of a " +
		"server-side configuration or permissions problem; no documents were searched"
	MsgRetrievalUnreachable = "the document store could not be reached; " +
		"no documents were searched"
	MsgRetrievalFailed = "the document search failed; no documents were searched"
)

// Message returns a description of err that is safe to send to an API
// client.
//
// Classification is by sentinel, using errors.Is, because
// pgedge-go-llm-lib wraps every provider failure in a *ProviderError
// whose Unwrap returns one of its sentinel values. The upstream
// Message and StatusCode are deliberately discarded: the whole point is
// that nothing derived from the provider's response body reaches the
// caller. Log the original error separately, via Redact, when detail is
// needed for diagnosis.
func Message(err error) string {
	if err == nil {
		return ""
	}

	// A retrieval failure is classified before it gets here, so its
	// message is chosen from the kind rather than from anything the
	// database said. It is checked first because such an error can wrap
	// a pgconn error that a later clause might otherwise claim: a
	// refused query on a dropped connection is still a refusal.
	var retrievalErr *database.RetrievalError
	if errors.As(err, &retrievalErr) {
		switch retrievalErr.Kind {
		case database.FailureRefused:
			return MsgRetrievalRefused
		case database.FailureUnreachable:
			return MsgRetrievalUnreachable
		default:
			return MsgRetrievalFailed
		}
	}

	switch {
	case errors.Is(err, llmlib.ErrAuthentication):
		return MsgAuthentication
	case errors.Is(err, llmlib.ErrRateLimit):
		return MsgRateLimit
	case errors.Is(err, llmlib.ErrInvalidRequest):
		return MsgInvalidRequest
	case errors.Is(err, llmlib.ErrNotSupported):
		return MsgNotSupported
	case errors.Is(err, llmlib.ErrProviderError):
		return MsgProvider
	case errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled):
		return MsgTimeout
	}

	// A transport-level failure is worth distinguishing from an internal
	// one, since reporting a provider outage as "internal" points an
	// operator at the wrong system. The message names neither host nor
	// port: a configured base_url may be an internal gateway whose
	// address is not something to publish on an unauthenticated endpoint.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return MsgUnreachable
	}
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) {
		return MsgUnreachable
	}

	// An unrecognised error may still wrap a provider response
	// somewhere in its chain, so the default must be generic rather
	// than falling back to err.Error().
	return MsgInternal
}

// secretPatterns match credential-shaped substrings. Each is
// intentionally broad on the trailing character class, since the aim is
// to catch a key however a provider chose to truncate or mask it.
var secretPatterns = []*regexp.Regexp{
	// OpenAI and compatible: sk-..., sk-proj-..., including masked
	// forms such as sk-proj-****************abcd.
	regexp.MustCompile(`sk-[A-Za-z0-9_\-*]{6,}`),
	// Anthropic: sk-ant-... (also matched above, kept for clarity if
	// the prefix ever changes shape).
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-*]{6,}`),
	// Voyage.
	regexp.MustCompile(`pa-[A-Za-z0-9_\-*]{10,}`),
	// Google AI Studio / Gemini.
	regexp.MustCompile(`AIza[A-Za-z0-9_\-*]{10,}`),
	// Bearer tokens in a relayed request or error.
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-*]{8,}`),
	// PostgreSQL connection strings: a libpq error can echo the DSN,
	// which carries the database password.
	regexp.MustCompile(`(?i)password=[^\s]+`),
}

// Placeholder replaces any credential-shaped substring found by Redact.
const Placeholder = "[redacted]"

// Redact removes credential-shaped substrings from s so that error
// detail can be logged without writing a secret to the log.
//
// This is best-effort pattern matching and is not a substitute for
// Message: use it for logs, never to sanitise something on its way to a
// client. A provider that invents a new key format, or echoes a key with
// no recognisable prefix, will defeat it.
func Redact(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, Placeholder)
	}
	return s
}

// RedactError returns Redact applied to err.Error(), or an empty string
// if err is nil. Convenience for logging call sites.
func RedactError(err error) string {
	if err == nil {
		return ""
	}
	return Redact(err.Error())
}
