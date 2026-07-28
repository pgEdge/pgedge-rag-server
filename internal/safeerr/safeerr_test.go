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
	"testing"

	llmlib "github.com/pgEdge/pgedge-go-llm-lib/llm"
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
