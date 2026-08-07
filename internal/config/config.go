//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package config handles configuration loading and validation for the
// pgEdge RAG Server.
package config

import (
	"fmt"
	"time"
)

// Duration is a time.Duration that unmarshals from a YAML string such
// as "90s" or "2m". An empty or absent value unmarshals to zero, which
// callers treat as "use the default". Representing timeouts as strings
// keeps the configuration human-readable rather than forcing raw
// nanosecond integers.
type Duration time.Duration

// Std returns the value as a standard time.Duration.
func (d Duration) Std() time.Duration {
	return time.Duration(d)
}

// UnmarshalYAML parses a duration string (e.g. "90s") into a Duration.
// An empty string is permitted and yields zero.
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if s == "" {
		*d = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Config is the root configuration structure for the server.
type Config struct {
	Server    ServerConfig   `yaml:"server"`
	Identity  IdentityConfig `yaml:"identity"`
	APIKeys   APIKeysConfig  `yaml:"api_keys"`
	Defaults  Defaults       `yaml:"defaults"`
	Pipelines []Pipeline     `yaml:"pipelines"`
}

// APIKeysConfig contains paths to files containing API keys for LLM providers.
// If not specified, keys are loaded from environment variables or default
// file locations (~/.anthropic-api-key, ~/.openai-api-key, ~/.voyage-api-key,
// ~/.gemini-api-key).
type APIKeysConfig struct {
	Anthropic string `yaml:"anthropic"` // Path to file containing Anthropic API key
	OpenAI    string `yaml:"openai"`    // Path to file containing OpenAI API key
	Voyage    string `yaml:"voyage"`    // Path to file containing Voyage API key
	Gemini    string `yaml:"gemini"`    // Path to file containing Gemini API key
}

// Identity defaults. These are the values used when the corresponding
// field is left unset and identity is enabled.
const (
	// DefaultClaimsHeader is the request header whose value is the
	// caller's verified claim set, as a JSON object.
	DefaultClaimsHeader = "X-Forwarded-Claims"

	// DefaultSubjectHeader is the request header carrying a bare subject
	// string, used only when the claims header is absent.
	DefaultSubjectHeader = "X-Forwarded-User"

	// DefaultClaimsSetting is the PostgreSQL run-time parameter the
	// claim set is written to for the duration of a query. This is the
	// name PostgREST uses, so policies already written for PostgREST
	// work unchanged.
	DefaultClaimsSetting = "request.jwt.claims"

	// DefaultSubjectClaim is the claim read out of the claim set to
	// label the request in logs. It is not used for authorisation — the
	// database decides that — only for operator visibility.
	DefaultSubjectClaim = "sub"

	// DefaultRoleClaim is the claim naming a PostgreSQL role to assume
	// for the query. A role is only ever assumed if it also appears in
	// allowed_roles, so this defaulting cannot by itself switch roles.
	DefaultRoleClaim = "role"
)

// Enforcement check modes for identity.enforcement_check.
const (
	EnforcementCheckError = "error"
	EnforcementCheckWarn  = "warn"
	EnforcementCheckOff   = "off"
)

// IdentityConfig controls whether retrieval runs as the caller rather
// than as the service's own database role.
//
// # The trust boundary
//
// The claims this server acts on arrive in ordinary HTTP request
// headers. This server does not verify them: it does not check a
// signature, does not fetch a JWKS, and does not validate an issuer,
// audience or expiry. It treats the configured headers as already
// verified, and applies them to the database session.
//
// That means the security of everything below depends on one
// assumption, which is now load-bearing:
//
//	Only a trusted component may set the claims and subject headers on
//	a request that reaches this server. That component must strip any
//	inbound copy of those headers from client input and re-set them
//	from a credential it has itself verified.
//
// In practice that component is an ingress proxy, API gateway or
// service mesh sidecar. If a caller can reach this server's listening
// port directly, that caller can assert any identity it likes, and
// row-level security will faithfully enforce the identity it was
// handed. Bind the server to a private interface, and set
// trusted_proxies so the peer address is checked as well.
//
// See docs/identity.md for the full deployment contract.
type IdentityConfig struct {
	// Enabled turns per-request identity on. When false (the default)
	// the server behaves exactly as it did before this option existed:
	// every query runs as the pipeline's configured database role, and
	// no identity is read from or required of a request.
	//
	// When true, a request that carries no identity is refused. There is
	// deliberately no fallback to the service's own role — falling back
	// would reintroduce the shared-role bypass invisibly, which is the
	// defect this option exists to remove.
	Enabled bool `yaml:"enabled"`

	// ClaimsHeader names the request header carrying the caller's
	// verified claims as a JSON object, e.g.
	// {"sub":"alice","role":"rag_tenant"}. Defaults to
	// DefaultClaimsHeader. Configurable because the header a given
	// ingress emits is a property of that deployment, not of this
	// server.
	ClaimsHeader string `yaml:"claims_header"`

	// SubjectHeader names a fallback header carrying a bare subject
	// string, for proxies that can assert who the caller is but cannot
	// emit a JSON claim set. Its value is wrapped as
	// {"<subject_claim>":"<value>"}. Only consulted when ClaimsHeader is
	// absent or empty. Defaults to DefaultSubjectHeader. Set to "-" to
	// disable the fallback and require a full claim set.
	SubjectHeader string `yaml:"subject_header"`

	// ClaimsSetting is the PostgreSQL run-time parameter the claim set
	// is written to, with SET LOCAL semantics, for the duration of each
	// query. Defaults to DefaultClaimsSetting.
	ClaimsSetting string `yaml:"claims_setting"`

	// SubjectClaim names the claim used to label requests in the log,
	// and the key used to wrap SubjectHeader. Defaults to
	// DefaultSubjectClaim.
	SubjectClaim string `yaml:"subject_claim"`

	// RoleClaim names the claim that may request a PostgreSQL role for
	// the query, in the manner of PostgREST. Defaults to
	// DefaultRoleClaim. Set to "-" to ignore role claims entirely.
	RoleClaim string `yaml:"role_claim"`

	// AllowedRoles is the set of PostgreSQL roles a request may be
	// switched to. An empty list (the default) disables role switching
	// completely: a request whose claims name a role is refused rather
	// than served with the role ignored, so a deployment cannot
	// half-configure this and believe role switching is in effect.
	//
	// The allowlist lives here rather than being left to the proxy on
	// purpose. The proxy decides who the caller is; this bounds what
	// that decision can reach in the database, so a compromised or
	// misconfigured proxy cannot name postgres and get it.
	AllowedRoles []string `yaml:"allowed_roles"`

	// TrustedProxies is a list of CIDR blocks. When non-empty, a
	// request whose immediate peer address falls outside every block is
	// refused before its headers are read at all.
	//
	// This checks the TCP peer, not X-Forwarded-For, because the peer
	// address is the only part of a request a client cannot choose. It
	// is a second line behind network policy, not a replacement for it.
	// Empty (the default) disables the check and logs a warning at
	// startup.
	TrustedProxies []string `yaml:"trusted_proxies"`

	// EnforcementCheck controls the startup preflight that verifies the
	// database will actually enforce the identity this server presents.
	// See database.VerifyEnforcement for what is checked.
	//
	//   error (default) — refuse to start if enforcement cannot be
	//                     confirmed for a configured table
	//   warn            — log loudly and start anyway
	//   off             — skip the check
	//
	// The default is deliberately fatal. Every failure this check
	// detects is one where retrieval appears to work and returns rows,
	// whilst row-level security is either absent or evaluating an
	// identity other than the caller's. There is no symptom to notice
	// in production, so the only safe moment to notice is startup.
	EnforcementCheck string `yaml:"enforcement_check"`

	// AllowSharedVectorIndex permits per-identity retrieval to use an
	// approximate vector index (HNSW/IVFFlat) that is shared between
	// identities.
	//
	// Default false, which forces an exact scan for the vector arm by
	// disabling index and bitmap scans for the query's transaction.
	//
	// This is not a performance knob, it is a disclosure one. pgvector
	// applies row-level security as a filter on top of the index scan,
	// after the index has already chosen candidates from the whole
	// corpus. When a caller's own rows are a minority, the scan's
	// candidate budget is spent on rows she may not see and the query
	// returns fewer rows than asked for — sometimes none. The size of
	// that shortfall, and the query's latency, are both functions of how
	// many rows the caller may NOT see lie near her query vector. A
	// caller who chooses query vectors can use that to map another
	// tenant's corpus in embedding space, and embedding inversion turns
	// a position in that space back into approximate text.
	//
	// No forbidden row is ever returned; the filtering works. The
	// filtering is what produces the signal, so filtering harder makes
	// it worse rather than better. Setting this true is only safe when
	// every identity that can reach a given table is permitted to know
	// the shape of everything in it — a single-tenant corpus split by
	// document category, say, rather than a corpus split by customer.
	AllowSharedVectorIndex bool `yaml:"allow_shared_vector_index"`
}

// Disabled is the sentinel value for SubjectHeader and RoleClaim that
// turns the corresponding mechanism off entirely, as distinct from
// leaving the field empty, which selects the default.
const Disabled = "-"

// WithDefaults returns a copy of the identity configuration with unset
// fields filled in.
//
// It returns a copy rather than mutating in place so it can be applied
// idempotently wherever the configuration is consumed. A Config built in
// code (a test, an embedding of this server) then behaves the same as
// one loaded from YAML, instead of silently running with empty header
// names.
func (c IdentityConfig) WithDefaults() IdentityConfig {
	if c.ClaimsHeader == "" {
		c.ClaimsHeader = DefaultClaimsHeader
	}
	if c.SubjectHeader == "" {
		c.SubjectHeader = DefaultSubjectHeader
	}
	if c.ClaimsSetting == "" {
		c.ClaimsSetting = DefaultClaimsSetting
	}
	if c.SubjectClaim == "" {
		c.SubjectClaim = DefaultSubjectClaim
	}
	if c.RoleClaim == "" {
		c.RoleClaim = DefaultRoleClaim
	}
	if c.EnforcementCheck == "" {
		c.EnforcementCheck = EnforcementCheckError
	}
	return c
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	ListenAddress string     `yaml:"listen_address"`
	Port          int        `yaml:"port"`
	TLS           TLSConfig  `yaml:"tls"`
	CORS          CORSConfig `yaml:"cors"`
}

// CORSConfig contains CORS (Cross-Origin Resource Sharing) settings.
type CORSConfig struct {
	Enabled        bool     `yaml:"enabled"`
	AllowedOrigins []string `yaml:"allowed_origins"` // Origins to allow, or ["*"] for all
}

// TLSConfig contains TLS/HTTPS settings.
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
}

// Defaults contains default values that can be overridden per-pipeline.
type Defaults struct {
	TokenBudget  int               `yaml:"token_budget"`
	TopN         int               `yaml:"top_n"`
	EmbeddingLLM LLMConfig         `yaml:"embedding_llm"` // Default embedding provider
	RAGLLM       LLMConfig         `yaml:"rag_llm"`       // Default completion provider
	APIKeys      APIKeysConfig     `yaml:"api_keys"`      // Default API key paths
	LLMHeaders   map[string]string `yaml:"llm_headers"`   // Default headers for LLM calls
}

// Pipeline defines a single RAG pipeline configuration.
type Pipeline struct {
	Name         string            `yaml:"name"`
	Description  string            `yaml:"description"`
	Database     DatabaseConfig    `yaml:"database"`
	Tables       []TableSource     `yaml:"tables"`
	EmbeddingLLM LLMConfig         `yaml:"embedding_llm"`
	RAGLLM       LLMConfig         `yaml:"rag_llm"`
	APIKeys      APIKeysConfig     `yaml:"api_keys"` // Pipeline-specific API key paths
	TokenBudget  int               `yaml:"token_budget"`
	TopN         int               `yaml:"top_n"`
	SystemPrompt string            `yaml:"system_prompt"` // Custom system prompt for LLM
	Search       SearchConfig      `yaml:"search"`        // Search behavior settings
	Rerank       RerankConfig      `yaml:"rerank"`        // Optional reranking stage
	LLMHeaders   map[string]string `yaml:"llm_headers"`   // Pipeline-level headers for LLM calls

	// AllowIncludeSources permits clients of this pipeline to request
	// the raw content of retrieved documents via include_sources.
	// Defaults to false: a request alone is not sufficient authority to
	// echo stored document content back to the caller, since anything
	// reachable in a configured table becomes directly retrievable by
	// whoever can reach the query endpoint. Deliberately not settable
	// under `defaults`, so that exposing a corpus is always an explicit
	// per-pipeline decision rather than something inherited.
	AllowIncludeSources bool `yaml:"allow_include_sources"`
}

// HostEntry represents a single host in a multi-host database configuration.
type HostEntry struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// DatabaseConfig contains PostgreSQL connection settings.
type DatabaseConfig struct {
	// Single-host connection fields
	Host string `yaml:"host"`
	Port int    `yaml:"port"`

	// Multi-host connection fields (for HA deployments)
	Hosts              []HostEntry `yaml:"hosts"`
	TargetSessionAttrs string      `yaml:"target_session_attrs"`

	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	SSLMode  string `yaml:"ssl_mode"`

	// Certificate-based authentication
	SSLCert   string `yaml:"ssl_cert"`
	SSLKey    string `yaml:"ssl_key"`
	SSLRootCA string `yaml:"ssl_root_ca"`
}

// TableSource defines a table with text and vector columns for hybrid search.
type TableSource struct {
	Table        string        `yaml:"table"`
	TextColumn   string        `yaml:"text_column"`
	VectorColumn string        `yaml:"vector_column"`
	IDColumn     string        `yaml:"id_column"` // Optional ID column (required for views)
	Filter       *ConfigFilter `yaml:"filter"`    // Optional filter (raw SQL or structured)
}

// SearchConfig contains settings for search behavior.
type SearchConfig struct {
	HybridEnabled *bool    `yaml:"hybrid_enabled"` // Enable hybrid search (default: true)
	VectorWeight  *float64 `yaml:"vector_weight"`  // Weight for vector vs BM25 (default: 0.5)
	MinSimilarity *float64 `yaml:"min_similarity"` // Minimum cosine similarity threshold (0.0-1.0)

	// BM25MaxDocuments caps how many rows the keyword-search arm reads
	// from a table per request (default: DefaultBM25MaxDocuments).
	//
	// The BM25 arm has no server-side ranking to push down, so it reads
	// rows matching the filter and ranks them in memory. Without a cap
	// that is an unbounded read of the whole table on every request,
	// which lets any caller who can reach the query endpoint impose work
	// proportional to table size rather than to request count. There is
	// no "unlimited" value: raise the number if you need a wider corpus,
	// accepting the cost that implies.
	BM25MaxDocuments *int `yaml:"bm25_max_documents"`
}

// RerankConfig contains settings for an optional reranking stage that
// reorders search results by relevance to the query immediately before
// context building. Leaving Provider empty (the default) disables the
// stage entirely. Only providers whose llm.Client.Rerank is actually
// implemented may be configured here (currently Voyage).
type RerankConfig struct {
	Provider string            `yaml:"provider"`
	Model    string            `yaml:"model"`
	BaseURL  string            `yaml:"base_url"` // Optional custom base URL
	Headers  map[string]string `yaml:"headers"`  // Per-rerank-call custom headers

	// RequestTimeout / PerAttemptTimeout behave as documented on
	// LLMConfig's fields of the same name.
	RequestTimeout    Duration `yaml:"request_timeout"`
	PerAttemptTimeout Duration `yaml:"per_attempt_timeout"`

	// TopK, when > 0, keeps only the top-K reranked results and
	// discards the rest before context building. Zero (the default)
	// reorders all retrieved results without dropping any.
	TopK int `yaml:"top_k"`
}

// FilterCondition represents a single filter condition.
type FilterCondition struct {
	Column   string      `json:"column" yaml:"column"`
	Operator string      `json:"operator" yaml:"operator"`
	Value    interface{} `json:"value" yaml:"value"`
}

// Filter represents a collection of conditions with logical operators.
// Used for API request filters which must be parameterized for security.
type Filter struct {
	Conditions []FilterCondition `json:"conditions" yaml:"conditions"`
	Logic      string            `json:"logic,omitempty" yaml:"logic,omitempty"` // "AND" or "OR", default "AND"
}

// ConfigFilter represents a filter in pipeline configuration.
// It can be either a raw SQL string (for admin use) or a structured Filter.
type ConfigFilter struct {
	RawSQL     string  // Raw SQL WHERE clause fragment (admin-only)
	Structured *Filter // Structured filter with conditions
}

// UnmarshalYAML implements custom YAML unmarshaling for ConfigFilter.
// Allows filter to be specified as either a string or structured object.
func (cf *ConfigFilter) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try string first (raw SQL)
	var s string
	if err := unmarshal(&s); err == nil {
		cf.RawSQL = s
		return nil
	}

	// Try structured filter
	var f Filter
	if err := unmarshal(&f); err == nil {
		cf.Structured = &f
		return nil
	}

	return fmt.Errorf("filter must be a string or structured filter object")
}

// LLMConfig contains settings for an LLM provider.
type LLMConfig struct {
	Provider string            `yaml:"provider"`
	Model    string            `yaml:"model"`
	BaseURL  string            `yaml:"base_url"` // Optional custom base URL (e.g. for API gateways)
	Headers  map[string]string `yaml:"headers"`  // Per-LLM custom headers

	// RequestTimeout caps the wall-clock time of a single request to
	// this provider, spanning every retry. Zero uses the library
	// default (120s). Specified as a duration string, e.g. "120s".
	RequestTimeout Duration `yaml:"request_timeout"`

	// PerAttemptTimeout, when greater than zero, bounds each individual
	// HTTP attempt so a single slow upstream (e.g. a heavy embedding
	// batch) is retried rather than burning the whole RequestTimeout
	// budget in one go. Set it below RequestTimeout to leave room for
	// retries. Zero disables per-attempt timeouts.
	PerAttemptTimeout Duration `yaml:"per_attempt_timeout"`
}

// DefaultConfig returns a Config with sensible default values.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddress: "0.0.0.0",
			Port:          8080,
			TLS: TLSConfig{
				Enabled: false,
			},
		},
		Defaults: Defaults{
			TokenBudget: 1000,
			TopN:        10,
		},
	}
}
