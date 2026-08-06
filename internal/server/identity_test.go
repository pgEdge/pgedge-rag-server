//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/identity"
	"github.com/pgEdge/pgedge-rag-server/internal/pipeline"
)

// identityConfig returns a test configuration with identity enabled.
func identityConfig(mutate func(*config.IdentityConfig)) *config.Config {
	cfg := testConfig()
	cfg.Identity = config.IdentityConfig{Enabled: true}.WithDefaults()
	if mutate != nil {
		mutate(&cfg.Identity)
	}
	return cfg
}

// queryRequest builds a POST to the query endpoint with the given
// headers.
func queryRequest(headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/pipelines/test-pipeline",
		strings.NewReader(`{"query":"what is pgEdge?"}`))
	r.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

// decodeError reads an ErrorResponse out of a recorded response.
func decodeError(t *testing.T, w *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()

	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("response body is not a JSON error: %v", err)
	}
	return resp
}

// TestRequireIdentity_RefusesUnidentifiedCallers is the HTTP half of the
// "no fallback to the service role" decision. Each case is a request
// that cannot be given an identity, and each gets its own status and
// code so the caller can tell which case they are in.
func TestRequireIdentity_RefusesUnidentifiedCallers(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*config.IdentityConfig)
		headers    map[string]string
		remoteAddr string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "no identity headers at all",
			wantStatus: http.StatusUnauthorized,
			wantCode:   codeIdentityRequired,
		},
		{
			name:       "claims that are not a JSON object",
			headers:    map[string]string{"X-Forwarded-Claims": "alice"},
			wantStatus: http.StatusBadRequest,
			wantCode:   codeIdentityMalformed,
		},
		{
			name: "a role claim that is not on the allowlist",
			headers: map[string]string{
				"X-Forwarded-Claims": `{"sub":"alice","role":"postgres"}`,
			},
			wantStatus: http.StatusForbidden,
			wantCode:   codeIdentityRoleDenied,
		},
		{
			name: "a peer outside the trusted proxies",
			mutate: func(c *config.IdentityConfig) {
				c.TrustedProxies = []string{"10.0.0.0/8"}
			},
			headers:    map[string]string{"X-Forwarded-Claims": `{"sub":"alice"}`},
			remoteAddr: "203.0.113.9:40000",
			wantStatus: http.StatusForbidden,
			wantCode:   codeIdentityUntrusted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := newMockPipelineManager()
			executed := false
			pm.pipelines["test-pipeline"].executor = &mockQueryExecutor{
				ExecuteWithOptionsFunc: func(
					ctx context.Context, req pipeline.QueryRequest,
				) (*pipeline.QueryResponse, error) {
					executed = true
					return &pipeline.QueryResponse{Answer: "should not happen"}, nil
				},
			}

			srv := mustNewServer(t, identityConfig(tt.mutate), pm)

			req := queryRequest(tt.headers)
			if tt.remoteAddr != "" {
				req.RemoteAddr = tt.remoteAddr
			}
			w := httptest.NewRecorder()
			srv.mux.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)",
					w.Code, tt.wantStatus, w.Body.String())
			}

			resp := decodeError(t, w)
			if resp.Error.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", resp.Error.Code, tt.wantCode)
			}

			// The refusal must happen before the pipeline runs. A request
			// that reached the pipeline has already spent an embedding
			// call, and — more to the point — would have queried the
			// corpus as the service role.
			if executed {
				t.Error("the pipeline executed for a request that was refused")
			}
		})
	}
}

// TestRequireIdentity_UntrustedPeerMessageIsNotDetailed checks that the
// refusal aimed at an untrusted peer does not describe the deployment's
// network topology to it.
func TestRequireIdentity_UntrustedPeerMessageIsNotDetailed(t *testing.T) {
	srv := mustNewServer(t, identityConfig(func(c *config.IdentityConfig) {
		c.TrustedProxies = []string{"10.0.0.0/8"}
	}), newMockPipelineManager())

	req := queryRequest(map[string]string{"X-Forwarded-Claims": `{"sub":"alice"}`})
	req.RemoteAddr = "203.0.113.9:40000"
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	message := decodeError(t, w).Error.Message
	for _, leak := range []string{"203.0.113.9", "10.0.0.0/8"} {
		if strings.Contains(message, leak) {
			t.Errorf("refusal message %q discloses %q", message, leak)
		}
	}
}

// TestRequireIdentity_PassesIdentityToThePipeline checks the accepting
// path: the claims reach the pipeline's context, so the database layer
// has something to apply.
func TestRequireIdentity_PassesIdentityToThePipeline(t *testing.T) {
	pm := newMockPipelineManager()

	var seen *identity.Identity
	pm.pipelines["test-pipeline"].executor = &mockQueryExecutor{
		ExecuteWithOptionsFunc: func(
			ctx context.Context, req pipeline.QueryRequest,
		) (*pipeline.QueryResponse, error) {
			seen, _ = identity.FromContext(ctx)
			return &pipeline.QueryResponse{Answer: "ok"}, nil
		},
	}

	srv := mustNewServer(t, identityConfig(func(c *config.IdentityConfig) {
		c.AllowedRoles = []string{"rag_tenant"}
	}), pm)

	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, queryRequest(map[string]string{
		"X-Forwarded-Claims": `{"sub":"alice","role":"rag_tenant"}`,
	}))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if seen == nil {
		t.Fatal("the pipeline was called with no identity in its context")
	}
	if seen.Claims != `{"sub":"alice","role":"rag_tenant"}` {
		t.Errorf("claims reaching the pipeline = %q", seen.Claims)
	}
	if seen.Subject != "alice" {
		t.Errorf("subject reaching the pipeline = %q, want %q", seen.Subject, "alice")
	}
	if seen.Role != "rag_tenant" {
		t.Errorf("role reaching the pipeline = %q, want %q", seen.Role, "rag_tenant")
	}
}

// TestRequireIdentity_DisabledLeavesRequestsAlone confirms the feature
// is opt-in: with identity off, a request with no headers is served
// exactly as before, and nothing is put in the context.
func TestRequireIdentity_DisabledLeavesRequestsAlone(t *testing.T) {
	pm := newMockPipelineManager()

	called := false
	var seen *identity.Identity
	pm.pipelines["test-pipeline"].executor = &mockQueryExecutor{
		ExecuteWithOptionsFunc: func(
			ctx context.Context, req pipeline.QueryRequest,
		) (*pipeline.QueryResponse, error) {
			called = true
			seen, _ = identity.FromContext(ctx)
			return &pipeline.QueryResponse{Answer: "ok"}, nil
		},
	}

	srv := mustNewServer(t, testConfig(), pm)

	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, queryRequest(nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if !called {
		t.Error("the pipeline was not called with identity disabled")
	}
	if seen != nil {
		t.Errorf("an identity appeared in the context with identity disabled: %+v", seen)
	}
}

// TestRequireIdentity_OnlyGuardsTheQueryEndpoint checks that enabling
// identity does not break the probe and discovery endpoints, which read
// no corpus. A liveness probe that started failing on a security change
// would take the deployment down.
func TestRequireIdentity_OnlyGuardsTheQueryEndpoint(t *testing.T) {
	srv := mustNewServer(t, identityConfig(nil), newMockPipelineManager())

	for _, path := range []string{
		"/v1/live", "/v1/health", "/v1/pipelines", "/v1/stats", "/v1/openapi.json",
	} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			srv.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			if w.Code != http.StatusOK {
				t.Errorf("status = %d for %s with identity enabled, want 200",
					w.Code, path)
			}
		})
	}
}

// TestHandlePipeline_RefusesWhenTheDatabaseLayerDoes covers the
// backstop: if a query somehow reaches the pipeline with no identity and
// the database layer refuses it, that must surface as a refusal rather
// than as an internal error, on both the streaming and non-streaming
// paths.
func TestHandlePipeline_RefusesWhenTheDatabaseLayerDoes(t *testing.T) {
	pm := newMockPipelineManager()
	pm.pipelines["test-pipeline"].executor = &mockQueryExecutor{
		ExecuteWithOptionsFunc: func(
			ctx context.Context, req pipeline.QueryRequest,
		) (*pipeline.QueryResponse, error) {
			return nil, identity.ErrRequired
		},
	}

	// Identity is disabled here, so requireIdentity does not intercept
	// and the handler's own check does not fire — leaving the executor's
	// error as the only thing that can produce the refusal. That is
	// exactly the path being tested.
	srv := mustNewServer(t, testConfig(), pm)

	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, queryRequest(nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body: %s)", w.Code, w.Body.String())
	}
	if code := decodeError(t, w).Error.Code; code != codeIdentityRequired {
		t.Errorf("error code = %q, want %q", code, codeIdentityRequired)
	}
}

// TestNew_RejectsUnusableIdentityConfig checks that the server refuses
// to start rather than starting with an identity check that cannot run.
func TestNew_RejectsUnusableIdentityConfig(t *testing.T) {
	cfg := identityConfig(func(c *config.IdentityConfig) {
		c.TrustedProxies = []string{"not-a-cidr"}
	})

	srv, err := New(cfg, newMockPipelineManager(), nil)
	if err == nil {
		t.Fatal("New accepted an identity configuration it cannot enforce")
	}
	if srv != nil {
		t.Error("New returned a server alongside an error")
	}
}

func TestClassifyIdentityError(t *testing.T) {
	tests := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{identity.ErrRequired, http.StatusUnauthorized, codeIdentityRequired},
		{identity.ErrUntrustedPeer, http.StatusForbidden, codeIdentityUntrusted},
		{identity.ErrMalformedClaims, http.StatusBadRequest, codeIdentityMalformed},
		{identity.ErrRoleNotAllowed, http.StatusForbidden, codeIdentityRoleDenied},
		// An unrecognised failure must still refuse, not admit.
		{errors.New("something else"), http.StatusUnauthorized, codeIdentityRequired},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			status, code := classifyIdentityError(tt.err)
			if status != tt.wantStatus || code != tt.wantCode {
				t.Errorf("classifyIdentityError(%v) = (%d, %q), want (%d, %q)",
					tt.err, status, code, tt.wantStatus, tt.wantCode)
			}
		})
	}
}
