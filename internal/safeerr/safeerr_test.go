//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package safeerr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	llmlib "github.com/pgEdge/pgedge-go-llm-lib/llm"

	"github.com/pgEdge/pgedge-rag-server/internal/database"
)

// leakedKey stands in for the truncated credential a provider echoes back
// on an authentication failure. Synthetic, obviously.
const leakedKey = "sk-proj-AAAABBBBCCCCDDDDEEEEFFFF0123"

// providerAuthError builds the error shape the LLM library actually
// produces for a rejected credential: a *ProviderError whose Message is
// the provider's own response body, and whose Unwrap returns a sentinel.
func providerAuthError() error {
	return &llmlib.ProviderError{
		Err:        llmlib.ErrAuthentication,
		StatusCode: 401,
		Provider:   "openai",
		Message: `{"error":{"message":"Incorrect API key provided: ` + leakedKey +
			`. You can find your API key at https://platform.openai.com/account/api-keys."}}`,
	}
}

// TestMessage_DoesNotLeakProviderBody is the core guard. The provider's
// error body reaches this server verbatim and contains part of a real
// API key; nothing derived from it may appear in what a client is told.
func TestMessage_DoesNotLeakProviderBody(t *testing.T) {
	err := providerAuthError()

	// Sanity check: the raw error really does carry the key, so this
	// test would be meaningless if it did not.
	if !strings.Contains(err.Error(), leakedKey) {
		t.Fatal("test setup is wrong: the raw error should contain the key")
	}

	got := Message(err)

	if strings.Contains(got, leakedKey) {
		t.Errorf("Message() leaked the API key: %q", got)
	}
	if strings.Contains(got, "sk-") {
		t.Errorf("Message() leaked a credential-shaped string: %q", got)
	}
	if strings.Contains(got, "Incorrect API key provided") {
		t.Errorf("Message() relayed the provider's response body: %q", got)
	}
	if got != MsgAuthentication {
		t.Errorf("Message() = %q, want %q", got, MsgAuthentication)
	}
}

// TestMessage_ClassifiesSentinels checks each sentinel maps to its own
// fixed description, so the endpoint stays useful for diagnosis without
// relaying anything upstream.
func TestMessage_ClassifiesSentinels(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"authentication", llmlib.ErrAuthentication, MsgAuthentication},
		{"rate limit", llmlib.ErrRateLimit, MsgRateLimit},
		{"invalid request", llmlib.ErrInvalidRequest, MsgInvalidRequest},
		{"not supported", llmlib.ErrNotSupported, MsgNotSupported},
		{"provider error", llmlib.ErrProviderError, MsgProvider},
		{"deadline exceeded", context.DeadlineExceeded, MsgTimeout},
		{"canceled", context.Canceled, MsgTimeout},
		{"unknown", errors.New("something went wrong internally"), MsgInternal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Message(tt.err); got != tt.want {
				t.Errorf("Message() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestMessage_UnknownErrorDoesNotEchoText pins the default. Falling back
// to err.Error() for an unrecognised error would reopen the leak for any
// provider failure the library has not classified.
func TestMessage_UnknownErrorDoesNotEchoText(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", errors.New("dsn password=hunter2 rejected "+leakedKey))

	got := Message(err)

	if strings.Contains(got, leakedKey) || strings.Contains(got, "hunter2") {
		t.Errorf("Message() echoed the underlying error text: %q", got)
	}
	if got != MsgInternal {
		t.Errorf("Message() = %q, want %q", got, MsgInternal)
	}
}

// TestMessage_WrappedProviderErrorStillClassified confirms classification
// survives the wrapping the orchestrator applies on the way up, since
// that is how the error actually arrives at the handler.
func TestMessage_WrappedProviderErrorStillClassified(t *testing.T) {
	err := fmt.Errorf("failed to generate completion: %w", providerAuthError())

	got := Message(err)

	if got != MsgAuthentication {
		t.Errorf("Message() = %q, want %q for a wrapped provider error", got, MsgAuthentication)
	}
	if strings.Contains(got, leakedKey) {
		t.Errorf("Message() leaked the API key through a wrapped error: %q", got)
	}
}

func TestRedact(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		mustDrop string
	}{
		{"openai key", "Incorrect API key provided: " + leakedKey, leakedKey},
		{"openai masked key", "provided: sk-proj-****************0123", "sk-proj-***"},
		{"anthropic key", "bad key sk-ant-api03-AAAABBBBCCCCDDDD", "sk-ant-api03-"},
		{"voyage key", "unauthorized: pa-AAAABBBBCCCCDDDDEEEE", "pa-AAAA"},
		{"gemini key", "invalid: AIzaAAAABBBBCCCCDDDDEEEE", "AIzaAAAA"},
		{"bearer token", "Authorization: Bearer AAAABBBBCCCCDDDD", "AAAABBBBCCCCDDDD"},
		{"dsn password", "failed to connect: password=hunter2 host=db", "hunter2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Redact(tt.in)
			if strings.Contains(got, tt.mustDrop) {
				t.Errorf("Redact(%q) = %q, still contains %q", tt.in, got, tt.mustDrop)
			}
			if !strings.Contains(got, Placeholder) {
				t.Errorf("Redact(%q) = %q, expected the placeholder", tt.in, got)
			}
		})
	}
}

func TestRedact_LeavesOrdinaryTextAlone(t *testing.T) {
	in := "failed to connect to database: dial tcp 192.0.2.1:5432: connection refused"

	if got := Redact(in); got != in {
		t.Errorf("Redact() altered text with no credential in it:\n got %q\nwant %q", got, in)
	}
}

func TestRedactError(t *testing.T) {
	if got := RedactError(nil); got != "" {
		t.Errorf("RedactError(nil) = %q, want empty string", got)
	}

	got := RedactError(providerAuthError())
	if strings.Contains(got, leakedKey) {
		t.Errorf("RedactError() left the key in the log text: %q", got)
	}
	// The point of RedactError is that the rest survives for diagnosis.
	if !strings.Contains(got, "Incorrect API key provided") {
		t.Errorf("RedactError() should keep non-secret detail for logs, got %q", got)
	}
}

// refusedRetrievalError builds the error the pipeline produces when the
// database refuses a configured table's search: a *RetrievalError whose
// wrapped cause carries the SQLSTATE, the table name and the SQL text.
// Everything in that cause is for the operator's log only.
func refusedRetrievalError() error {
	cause := fmt.Errorf(
		"vector search failed (SELECT id, content FROM public.docs "+
			"ORDER BY embedding <=> $1::vector LIMIT $2): %w",
		&pgconn.PgError{
			Severity:   "ERROR",
			Code:       "42501",
			Message:    "permission denied for table docs",
			TableName:  "docs",
			SchemaName: "public",
		},
	)
	return &database.RetrievalError{Kind: database.FailureRefused, Err: cause}
}

// TestMessage_RetrievalFailureKinds pins the three answers a caller can
// get for a failed retrieval (issue #49). Each must be distinct: the
// whole point is that a caller can tell a configuration problem from an
// outage without reading the operator's log.
func TestMessage_RetrievalFailureKinds(t *testing.T) {
	tests := []struct {
		name string
		kind database.FailureKind
		want string
	}{
		{"refused", database.FailureRefused, MsgRetrievalRefused},
		{"unreachable", database.FailureUnreachable, MsgRetrievalUnreachable},
		{"unknown", database.FailureUnknown, MsgRetrievalFailed},
	}

	seen := make(map[string]string, len(tests))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &database.RetrievalError{
				Kind: tt.kind,
				Err:  errors.New("permission denied for table docs"),
			}

			got := Message(err)
			if got != tt.want {
				t.Errorf("Message(%v) = %q, want %q", tt.kind, got, tt.want)
			}
			if prev, dup := seen[got]; dup {
				t.Errorf("kind %v shares its message with %v: %q", tt.kind, prev, got)
			}
			seen[got] = tt.name
		})
	}
}

// TestMessage_RetrievalFailureDoesNotLeakSchemaDetail is the constraint
// from issue #49: the operator's log carries the SQLSTATE, the table
// name and the SQL text; the caller gets none of them.
func TestMessage_RetrievalFailureDoesNotLeakSchemaDetail(t *testing.T) {
	err := refusedRetrievalError()

	// Sanity check: the raw error really does carry the detail, so this
	// test would be meaningless if it did not.
	for _, detail := range []string{"docs", "42501", "SELECT", "embedding"} {
		if !strings.Contains(err.Error(), detail) {
			t.Fatalf("test setup is wrong: the raw error should contain %q", detail)
		}
	}

	got := Message(err)

	for _, detail := range []string{
		"docs", "public", "42501", "SELECT", "embedding", "permission denied",
	} {
		if strings.Contains(got, detail) {
			t.Errorf("Message() leaked %q to the caller: %q", detail, got)
		}
	}
	if got != MsgRetrievalRefused {
		t.Errorf("expected the refused message, got %q", got)
	}
}

// TestMessage_RetrievalFailureOutranksTransportClassification checks the
// ordering inside Message: a refusal that happens to wrap a network-shaped
// error must still be reported as a refusal, not as "the provider could
// not be reached", which would send the operator after the wrong system.
func TestMessage_RetrievalFailureOutranksTransportClassification(t *testing.T) {
	err := &database.RetrievalError{
		Kind: database.FailureRefused,
		Err:  fmt.Errorf("closing connection: %w", syscall.ECONNRESET),
	}

	if got := Message(err); got != MsgRetrievalRefused {
		t.Errorf("Message() = %q, want %q", got, MsgRetrievalRefused)
	}
}

// TestRedactError_KeepsRetrievalDetailForTheLog is the other half of the
// contract: what the caller is denied, the operator must still get.
func TestRedactError_KeepsRetrievalDetailForTheLog(t *testing.T) {
	got := RedactError(refusedRetrievalError())

	for _, detail := range []string{"refused", "docs", "42501", "permission denied"} {
		if !strings.Contains(got, detail) {
			t.Errorf("log text lost %q, which the operator needs: %q", detail, got)
		}
	}
}
