//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

func request(headers map[string]string, remoteAddr string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/pipelines/docs", nil)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	if remoteAddr != "" {
		r.RemoteAddr = remoteAddr
	}
	return r
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.IdentityConfig
		headers     map[string]string
		remoteAddr  string
		wantErr     error
		wantClaims  string
		wantSubject string
		wantRole    string
	}{
		{
			name:        "claims header is passed through verbatim",
			headers:     map[string]string{"X-Forwarded-Claims": `{"sub":"alice","tenant":"acme"}`},
			wantClaims:  `{"sub":"alice","tenant":"acme"}`,
			wantSubject: "alice",
		},
		{
			name:        "subject header is wrapped as a claim set",
			headers:     map[string]string{"X-Forwarded-User": "alice"},
			wantClaims:  `{"sub":"alice"}`,
			wantSubject: "alice",
		},
		{
			name: "claims header wins over subject header",
			headers: map[string]string{
				"X-Forwarded-Claims": `{"sub":"alice"}`,
				"X-Forwarded-User":   "mallory",
			},
			wantClaims:  `{"sub":"alice"}`,
			wantSubject: "alice",
		},
		{
			name:    "no identity at all is refused",
			wantErr: ErrRequired,
		},
		{
			name:    "empty claims header falls through to no identity",
			headers: map[string]string{"X-Forwarded-Claims": ""},
			wantErr: ErrRequired,
		},
		{
			name:    "subject fallback can be turned off",
			cfg:     config.IdentityConfig{SubjectHeader: config.Disabled},
			headers: map[string]string{"X-Forwarded-User": "alice"},
			wantErr: ErrRequired,
		},
		{
			name:    "claims that are not JSON are refused",
			headers: map[string]string{"X-Forwarded-Claims": "alice"},
			wantErr: ErrMalformedClaims,
		},
		{
			name:    "claims that are a JSON array are refused",
			headers: map[string]string{"X-Forwarded-Claims": `["alice"]`},
			wantErr: ErrMalformedClaims,
		},
		{
			name:       "custom header names are honoured",
			cfg:        config.IdentityConfig{ClaimsHeader: "X-Tenant-Claims"},
			headers:    map[string]string{"X-Tenant-Claims": `{"sub":"alice"}`},
			wantClaims: `{"sub":"alice"}`,
			// The default claims header must no longer be consulted.
			wantSubject: "alice",
		},
		{
			name:       "the default header is ignored once one is configured",
			cfg:        config.IdentityConfig{ClaimsHeader: "X-Tenant-Claims", SubjectHeader: config.Disabled},
			headers:    map[string]string{"X-Forwarded-Claims": `{"sub":"alice"}`},
			wantErr:    ErrRequired,
			wantClaims: "",
		},
		{
			name: "a claimed role must be on the allowlist",
			cfg:  config.IdentityConfig{AllowedRoles: []string{"rag_tenant"}},
			headers: map[string]string{
				"X-Forwarded-Claims": `{"sub":"alice","role":"rag_tenant"}`,
			},
			wantClaims:  `{"sub":"alice","role":"rag_tenant"}`,
			wantSubject: "alice",
			wantRole:    "rag_tenant",
		},
		{
			name: "a role off the allowlist is refused",
			cfg:  config.IdentityConfig{AllowedRoles: []string{"rag_tenant"}},
			headers: map[string]string{
				"X-Forwarded-Claims": `{"sub":"alice","role":"postgres"}`,
			},
			wantErr: ErrRoleNotAllowed,
		},
		{
			name: "an empty allowlist refuses every role claim",
			headers: map[string]string{
				"X-Forwarded-Claims": `{"sub":"alice","role":"rag_tenant"}`,
			},
			wantErr: ErrRoleNotAllowed,
		},
		{
			name: "role claims can be ignored entirely",
			cfg:  config.IdentityConfig{RoleClaim: config.Disabled},
			headers: map[string]string{
				"X-Forwarded-Claims": `{"sub":"alice","role":"postgres"}`,
			},
			wantClaims:  `{"sub":"alice","role":"postgres"}`,
			wantSubject: "alice",
			wantRole:    "",
		},
		{
			name: "a non-string role claim is not a role",
			cfg:  config.IdentityConfig{AllowedRoles: []string{"rag_tenant"}},
			headers: map[string]string{
				"X-Forwarded-Claims": `{"sub":"alice","role":["rag_tenant"]}`,
			},
			wantClaims:  `{"sub":"alice","role":["rag_tenant"]}`,
			wantSubject: "alice",
			wantRole:    "",
		},
		{
			name: "a non-string subject claim does not become a subject",
			headers: map[string]string{
				"X-Forwarded-Claims": `{"sub":42}`,
			},
			wantClaims:  `{"sub":42}`,
			wantSubject: "",
		},
		{
			name:        "a custom subject claim is read",
			cfg:         config.IdentityConfig{SubjectClaim: "email"},
			headers:     map[string]string{"X-Forwarded-User": "alice@example.com"},
			wantClaims:  `{"email":"alice@example.com"}`,
			wantSubject: "alice@example.com",
		},
		{
			name:        "a trusted peer is admitted",
			cfg:         config.IdentityConfig{TrustedProxies: []string{"10.0.0.0/8"}},
			headers:     map[string]string{"X-Forwarded-Claims": `{"sub":"alice"}`},
			remoteAddr:  "10.1.2.3:40000",
			wantClaims:  `{"sub":"alice"}`,
			wantSubject: "alice",
		},
		{
			name:       "an untrusted peer is refused before its headers are read",
			cfg:        config.IdentityConfig{TrustedProxies: []string{"10.0.0.0/8"}},
			headers:    map[string]string{"X-Forwarded-Claims": `{"sub":"alice"}`},
			remoteAddr: "203.0.113.9:40000",
			wantErr:    ErrUntrustedPeer,
		},
		{
			name:       "an unparseable peer is refused when the check is on",
			cfg:        config.IdentityConfig{TrustedProxies: []string{"10.0.0.0/8"}},
			headers:    map[string]string{"X-Forwarded-Claims": `{"sub":"alice"}`},
			remoteAddr: "not-an-address",
			wantErr:    ErrUntrustedPeer,
		},
		{
			name:        "IPv6 peers are matched",
			cfg:         config.IdentityConfig{TrustedProxies: []string{"2001:db8::/32"}},
			headers:     map[string]string{"X-Forwarded-Claims": `{"sub":"alice"}`},
			remoteAddr:  "[2001:db8::1]:40000",
			wantClaims:  `{"sub":"alice"}`,
			wantSubject: "alice",
		},
		{
			name:        "a subject containing JSON metacharacters is escaped",
			headers:     map[string]string{"X-Forwarded-User": `al"ice","role":"postgres`},
			wantClaims:  `{"sub":"al\"ice\",\"role\":\"postgres"}`,
			wantSubject: `al"ice","role":"postgres`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extractor, err := NewExtractor(tt.cfg)
			if err != nil {
				t.Fatalf("NewExtractor failed: %v", err)
			}

			id, err := extractor.Extract(request(tt.headers, tt.remoteAddr))

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Extract returned %v, want %v", err, tt.wantErr)
				}
				if id != nil {
					t.Errorf("Extract returned an identity alongside an error: %+v", id)
				}
				return
			}

			if err != nil {
				t.Fatalf("Extract returned an unexpected error: %v", err)
			}
			if id.Claims != tt.wantClaims {
				t.Errorf("claims = %q, want %q", id.Claims, tt.wantClaims)
			}
			if id.Subject != tt.wantSubject {
				t.Errorf("subject = %q, want %q", id.Subject, tt.wantSubject)
			}
			if id.Role != tt.wantRole {
				t.Errorf("role = %q, want %q", id.Role, tt.wantRole)
			}
		})
	}
}

// TestExtract_SubjectWrappingIsValidJSON checks the escaping case above
// from the database's point of view: whatever a proxy puts in the
// subject header, what reaches the claims parameter must be a JSON
// object with exactly one key, and the subject must survive intact.
//
// This is the wrapping path's injection surface. A subject assembled by
// string concatenation would let a value containing a quote add claims
// of its own — a role claim, for instance.
func TestExtract_SubjectWrappingIsValidJSON(t *testing.T) {
	subjects := []string{
		`plain`,
		`al"ice`,
		`back\slash`,
		`","role":"postgres`,
		`{"sub":"other"}`,
		"new\nline",
		"emoji 🙂",
	}

	extractor, err := NewExtractor(config.IdentityConfig{})
	if err != nil {
		t.Fatalf("NewExtractor failed: %v", err)
	}

	for _, subject := range subjects {
		t.Run(subject, func(t *testing.T) {
			// A header value cannot itself contain a newline, so that case
			// exercises the encoder rather than the header parser.
			id, err := extractor.Extract(request(nil, ""))
			_ = id
			if !errors.Is(err, ErrRequired) {
				t.Fatalf("sanity check failed: %v", err)
			}

			r := httptest.NewRequest(http.MethodPost, "/v1/pipelines/docs", nil)
			r.Header["X-Forwarded-User"] = []string{subject}

			id, err = extractor.Extract(r)
			if err != nil {
				t.Fatalf("Extract failed: %v", err)
			}

			var decoded map[string]any
			if err := json.Unmarshal([]byte(id.Claims), &decoded); err != nil {
				t.Fatalf("claims %q are not valid JSON: %v", id.Claims, err)
			}
			if len(decoded) != 1 {
				t.Errorf("claims %q decoded to %d keys, want exactly 1",
					id.Claims, len(decoded))
			}
			if decoded["sub"] != subject {
				t.Errorf("sub = %v, want %q", decoded["sub"], subject)
			}
			if _, ok := decoded["role"]; ok {
				t.Errorf("a role claim appeared from a subject header: %q", id.Claims)
			}
		})
	}
}

func TestNewExtractor_RejectsMalformedTrustedProxies(t *testing.T) {
	_, err := NewExtractor(config.IdentityConfig{
		TrustedProxies: []string{"10.0.0.0/8", "not-a-cidr"},
	})
	if err == nil {
		t.Fatal("NewExtractor accepted a malformed CIDR; an ignored entry would " +
			"be an access check that silently does not run")
	}
}

func TestContextRoundTrip(t *testing.T) {
	want := &Identity{Claims: `{"sub":"alice"}`, Subject: "alice"}

	ctx := NewContext(context.Background(), want)
	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("FromContext did not find the identity that NewContext stored")
	}
	if got != want {
		t.Errorf("FromContext returned %+v, want %+v", got, want)
	}
}

func TestFromContext_EmptyAndNil(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Error("FromContext found an identity in a bare context")
	}

	// A nil identity stored explicitly must not read back as present:
	// the database layer's guard is a boolean check, and a non-nil "ok"
	// with a nil pointer would panic rather than refuse.
	ctx := NewContext(context.Background(), nil)
	if _, ok := FromContext(ctx); ok {
		t.Error("FromContext reported a nil identity as present")
	}
}
