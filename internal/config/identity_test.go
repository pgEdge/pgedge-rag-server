//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestIdentityWithDefaults(t *testing.T) {
	got := IdentityConfig{}.WithDefaults()

	if got.ClaimsHeader != DefaultClaimsHeader {
		t.Errorf("claims_header = %q, want %q", got.ClaimsHeader, DefaultClaimsHeader)
	}
	if got.SubjectHeader != DefaultSubjectHeader {
		t.Errorf("subject_header = %q, want %q", got.SubjectHeader, DefaultSubjectHeader)
	}
	if got.ClaimsSetting != DefaultClaimsSetting {
		t.Errorf("claims_setting = %q, want %q", got.ClaimsSetting, DefaultClaimsSetting)
	}
	if got.SubjectClaim != DefaultSubjectClaim {
		t.Errorf("subject_claim = %q, want %q", got.SubjectClaim, DefaultSubjectClaim)
	}
	if got.RoleClaim != DefaultRoleClaim {
		t.Errorf("role_claim = %q, want %q", got.RoleClaim, DefaultRoleClaim)
	}
	if got.EnforcementCheck != EnforcementCheckError {
		t.Errorf("enforcement_check = %q, want %q",
			got.EnforcementCheck, EnforcementCheckError)
	}

	// The two settings that decide whether anything is enforced must
	// default to the safe value, not merely to something.
	if got.Enabled {
		t.Error("identity defaulted to enabled; enabling it is a deployment " +
			"decision that requires database-side policies to be in place")
	}
	if got.AllowSharedVectorIndex {
		t.Error("allow_shared_vector_index defaulted to true; the default must " +
			"be the one that closes the cross-identity side channel")
	}
	if len(got.AllowedRoles) != 0 {
		t.Error("allowed_roles defaulted to a non-empty list")
	}
}

func TestIdentityWithDefaults_DoesNotOverrideOrMutate(t *testing.T) {
	original := IdentityConfig{
		ClaimsHeader:     "X-Tenant-Claims",
		SubjectHeader:    Disabled,
		ClaimsSetting:    "app.claims",
		RoleClaim:        Disabled,
		EnforcementCheck: EnforcementCheckWarn,
	}

	got := original.WithDefaults()

	if got.ClaimsHeader != "X-Tenant-Claims" {
		t.Errorf("claims_header was overwritten: %q", got.ClaimsHeader)
	}
	if got.SubjectHeader != Disabled {
		t.Errorf("an explicitly disabled subject_header was replaced by the "+
			"default: %q", got.SubjectHeader)
	}
	if got.RoleClaim != Disabled {
		t.Errorf("an explicitly disabled role_claim was replaced by the "+
			"default: %q", got.RoleClaim)
	}
	if got.ClaimsSetting != "app.claims" {
		t.Errorf("claims_setting was overwritten: %q", got.ClaimsSetting)
	}
	if got.EnforcementCheck != EnforcementCheckWarn {
		t.Errorf("enforcement_check was overwritten: %q", got.EnforcementCheck)
	}

	// WithDefaults is applied at several call sites, so it must be a
	// pure function of its receiver.
	if original.ClaimsSetting != "app.claims" || original.SubjectClaim != "" {
		t.Error("WithDefaults mutated its receiver")
	}
	if second := got.WithDefaults(); !reflect.DeepEqual(second, got) {
		t.Error("WithDefaults is not idempotent")
	}
}

func TestValidateIdentity(t *testing.T) {
	tests := []struct {
		name      string
		identity  IdentityConfig
		wantField string
	}{
		{
			name:     "a default configuration validates",
			identity: IdentityConfig{Enabled: true},
		},
		{
			name: "a fully specified configuration validates",
			identity: IdentityConfig{
				Enabled:          true,
				ClaimsHeader:     "X-Tenant-Claims",
				SubjectHeader:    Disabled,
				ClaimsSetting:    "app.jwt.claims",
				SubjectClaim:     "email",
				RoleClaim:        Disabled,
				AllowedRoles:     []string{"rag_tenant", "rag_admin"},
				TrustedProxies:   []string{"10.0.0.0/8", "2001:db8::/32"},
				EnforcementCheck: EnforcementCheckWarn,
			},
		},
		{
			name: "a header name with a space is rejected",
			identity: IdentityConfig{
				Enabled:      true,
				ClaimsHeader: "X Forwarded Claims",
			},
			wantField: "identity.claims_header",
		},
		{
			name: "a subject header name with a colon is rejected",
			identity: IdentityConfig{
				Enabled:       true,
				SubjectHeader: "X-User:",
			},
			wantField: "identity.subject_header",
		},
		{
			name: "an unqualified claims setting is rejected",
			identity: IdentityConfig{
				Enabled:       true,
				ClaimsSetting: "claims",
			},
			wantField: "identity.claims_setting",
		},
		{
			name: "a claims setting with a quote is rejected",
			identity: IdentityConfig{
				Enabled:       true,
				ClaimsSetting: `request.jwt.claims'`,
			},
			wantField: "identity.claims_setting",
		},
		{
			name: "a disabled subject claim is rejected",
			identity: IdentityConfig{
				Enabled:      true,
				SubjectClaim: Disabled,
			},
			wantField: "identity.subject_claim",
		},
		{
			name: "a role name needing quotes is rejected",
			identity: IdentityConfig{
				Enabled:      true,
				AllowedRoles: []string{"rag tenant"},
			},
			wantField: "identity.allowed_roles[0]",
		},
		{
			name: "a bare address in trusted_proxies is rejected",
			identity: IdentityConfig{
				Enabled:        true,
				TrustedProxies: []string{"10.0.0.1"},
			},
			wantField: "identity.trusted_proxies[0]",
		},
		{
			name: "an unknown enforcement_check is rejected",
			identity: IdentityConfig{
				Enabled:          true,
				EnforcementCheck: "maybe",
			},
			wantField: "identity.enforcement_check",
		},
		{
			// Nothing under identity is read when it is off, and
			// rejecting a prepared-but-disabled block would break
			// configurations that stage the change before enabling it.
			name: "nothing is validated while identity is disabled",
			identity: IdentityConfig{
				Enabled:          false,
				ClaimsHeader:     "X Forwarded Claims",
				EnforcementCheck: "maybe",
				TrustedProxies:   []string{"nonsense"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Identity: tt.identity}
			errs := cfg.validateIdentity()

			if tt.wantField == "" {
				if len(errs) != 0 {
					t.Fatalf("expected no validation errors, got: %v", errs)
				}
				return
			}

			var fields []string
			for _, e := range errs {
				fields = append(fields, e.Field)
			}
			if !containsField(fields, tt.wantField) {
				t.Errorf("expected a validation error on %q, got errors on %v",
					tt.wantField, fields)
			}
		})
	}
}

// TestValidateIdentity_ReachesTopLevelValidate confirms the identity
// checks are actually wired into Config.Validate, rather than only being
// callable. A validation function nobody calls is not a control.
func TestValidateIdentity_ReachesTopLevelValidate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Identity = IdentityConfig{Enabled: true, EnforcementCheck: "maybe"}
	cfg.Pipelines = []Pipeline{{
		Name:         "docs",
		Database:     DatabaseConfig{Host: "localhost", Port: 5432, Database: "ragdb"},
		Tables:       []TableSource{{Table: "chunks", TextColumn: "content", VectorColumn: "embedding"}},
		EmbeddingLLM: LLMConfig{Provider: "openai", Model: "text-embedding-3-small"},
		RAGLLM:       LLMConfig{Provider: "anthropic", Model: "claude-sonnet-4-5"},
	}}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate accepted an invalid identity.enforcement_check")
	}
	if !strings.Contains(err.Error(), "identity.enforcement_check") {
		t.Errorf("Validate error does not mention the offending field: %v", err)
	}
}

func containsField(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
