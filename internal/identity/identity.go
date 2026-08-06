//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package identity carries the identity of the caller who made a
// request from the HTTP layer down to the database layer, so that a
// retrieval can run as that caller rather than as the service's own
// database role.
//
// The model is PostgREST's: take the verified claims that arrive with
// the request, apply them to the database session for the lifetime of
// that one query, and let row-level security decide what the query may
// see. Nothing in this package makes an authorisation decision. It
// presents an identity; PostgreSQL enforces the policies written
// against it.
//
// # What this package trusts
//
// It trusts the configured request headers completely. It does not
// verify a signature, fetch a JWKS, or check an issuer, audience or
// expiry. Whatever sits in front of this server is responsible for
// having done that, and for stripping any client-supplied copy of those
// headers before setting its own. See config.IdentityConfig for the
// full statement of that assumption, and docs/identity.md for the
// deployment contract it implies.
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

// Errors returned by Extractor.Extract and by the database layer when
// identity is required. Each is distinct so a caller can tell which
// case it is in: "you sent no identity" and "the identity you sent
// named a role you may not have" are different problems with different
// fixes, and collapsing them into one 401 would leave an operator
// guessing.
var (
	// ErrRequired means identity is enabled but the request carried
	// none. It is also what the database layer returns when it is asked
	// to run a query with no identity in the context, which is the
	// backstop for a code path that bypasses the HTTP middleware.
	ErrRequired = errors.New("request carries no caller identity")

	// ErrUntrustedPeer means the request's immediate peer address is
	// outside every configured trusted_proxies block, so its identity
	// headers were not read at all.
	ErrUntrustedPeer = errors.New("request peer is not a trusted proxy")

	// ErrMalformedClaims means the claims header was present but was
	// not a JSON object.
	ErrMalformedClaims = errors.New("claims header is not a JSON object")

	// ErrRoleNotAllowed means the claims named a database role that is
	// not in allowed_roles. The request is refused rather than served
	// with the role ignored: silently downgrading to no role switch
	// would run the query with more or less access than the claim asked
	// for, and the caller would have no way to tell.
	ErrRoleNotAllowed = errors.New("claimed database role is not permitted")
)

// Identity is the caller's identity for one request.
type Identity struct {
	// Claims is the caller's claim set as JSON object text, exactly as
	// it will be written to the database's claims parameter. Held as
	// text rather than a decoded map so that what row-level security
	// sees is byte-for-byte what the proxy asserted: re-encoding a
	// decoded map would silently normalise number formats and key
	// order, and a policy that digs into a nested claim would then be
	// evaluating something this server invented.
	Claims string

	// Subject is the value of the configured subject claim, if it was
	// present and a string. Used only to label requests in the log.
	Subject string

	// Role, when non-empty, is a PostgreSQL role to assume for the
	// query. It has already been checked against allowed_roles.
	Role string
}

// contextKey is unexported so no other package can put a value under
// this key, and an identity in a context can only have come from here.
type contextKey struct{}

// NewContext returns a copy of ctx carrying id.
func NewContext(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext returns the identity carried by ctx, if any.
func FromContext(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(contextKey{}).(*Identity)
	if !ok || id == nil {
		return nil, false
	}
	return id, true
}

// Extractor reads a caller identity out of an HTTP request according to
// the identity configuration.
type Extractor struct {
	cfg     config.IdentityConfig
	trusted []*net.IPNet
}

// NewExtractor builds an Extractor from configuration. It fails on a
// malformed trusted_proxies entry rather than ignoring it, because an
// ignored CIDR is an access check that silently does not run.
func NewExtractor(cfg config.IdentityConfig) (*Extractor, error) {
	cfg = cfg.WithDefaults()

	trusted := make([]*net.IPNet, 0, len(cfg.TrustedProxies))
	for _, entry := range cfg.TrustedProxies {
		_, block, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted_proxies entry %q: %w", entry, err)
		}
		trusted = append(trusted, block)
	}

	return &Extractor{cfg: cfg, trusted: trusted}, nil
}

// Extract reads the caller's identity from r.
//
// Order of business, and why: the peer check runs first so that an
// untrusted peer's headers are never parsed or logged at all; the
// claims header wins over the subject header so a proxy that can assert
// a full claim set is never silently downgraded; and the role check
// runs last so that a refusal names the role rather than the request.
func (e *Extractor) Extract(r *http.Request) (*Identity, error) {
	if err := e.checkPeer(r.RemoteAddr); err != nil {
		return nil, err
	}

	claims, err := e.readClaims(r)
	if err != nil {
		return nil, err
	}

	decoded := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(claims), &decoded); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformedClaims, err)
	}

	id := &Identity{
		Claims:  claims,
		Subject: stringClaim(decoded, e.cfg.SubjectClaim),
	}

	role, err := e.resolveRole(decoded)
	if err != nil {
		return nil, err
	}
	id.Role = role

	return id, nil
}

// checkPeer verifies the request's immediate peer against
// trusted_proxies. An empty list disables the check.
//
// This deliberately looks at the TCP peer rather than at any
// X-Forwarded-For chain: the peer address is the one part of a request
// a client cannot choose, and a forwarding header is exactly as
// trustworthy as the identity headers this check exists to gate.
func (e *Extractor) checkPeer(remoteAddr string) error {
	if len(e.trusted) == 0 {
		return nil
	}

	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// No port, or an address the net package cannot parse. Fall back
		// to treating the whole string as an address, and refuse if that
		// does not parse either — an unparseable peer cannot be shown to
		// be trusted.
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("%w: cannot parse peer address %q", ErrUntrustedPeer, remoteAddr)
	}

	for _, block := range e.trusted {
		if block.Contains(ip) {
			return nil
		}
	}

	return fmt.Errorf("%w: %s", ErrUntrustedPeer, ip)
}

// readClaims returns the caller's claim set as JSON object text, from
// the claims header if present, otherwise from the subject header.
func (e *Extractor) readClaims(r *http.Request) (string, error) {
	if raw := r.Header.Get(e.cfg.ClaimsHeader); raw != "" {
		return raw, nil
	}

	if e.cfg.SubjectHeader == config.Disabled {
		return "", fmt.Errorf("%w: no %s header", ErrRequired, e.cfg.ClaimsHeader)
	}

	subject := r.Header.Get(e.cfg.SubjectHeader)
	if subject == "" {
		return "", fmt.Errorf("%w: no %s or %s header",
			ErrRequired, e.cfg.ClaimsHeader, e.cfg.SubjectHeader)
	}

	// Built by the JSON encoder rather than by string concatenation, so
	// a subject containing a quote or a backslash cannot alter the shape
	// of the claim set the database is handed.
	encoded, err := json.Marshal(map[string]string{e.cfg.SubjectClaim: subject})
	if err != nil {
		return "", fmt.Errorf("failed to encode subject claim: %w", err)
	}
	return string(encoded), nil
}

// resolveRole returns the database role the request should assume, or
// "" for none.
func (e *Extractor) resolveRole(claims map[string]json.RawMessage) (string, error) {
	if e.cfg.RoleClaim == config.Disabled {
		return "", nil
	}

	role := stringClaim(claims, e.cfg.RoleClaim)
	if role == "" {
		return "", nil
	}

	if !slices.Contains(e.cfg.AllowedRoles, role) {
		return "", fmt.Errorf("%w: %q", ErrRoleNotAllowed, role)
	}

	return role, nil
}

// stringClaim returns the named claim's value when it is present and is
// a JSON string, and "" otherwise. A non-string claim is treated as
// absent rather than coerced: a role claim of 42 or ["postgres"] is not
// a role name, and turning it into one would be inventing an identity.
func stringClaim(claims map[string]json.RawMessage, name string) string {
	raw, ok := claims[name]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}
