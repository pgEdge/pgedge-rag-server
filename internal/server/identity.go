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
	"errors"
	"fmt"
	"net/http"

	"github.com/pgEdge/pgedge-rag-server/internal/identity"
)

// Error codes returned when a request cannot be given a caller
// identity. They are distinct rather than one blanket 401 so that a
// caller — or the operator reading its logs — can tell which case they
// are in without guessing: "you sent nothing", "you are not permitted
// to assert an identity from where you are", "what you sent was not
// claims", and "the role you claimed is not on the allowlist" have four
// different fixes, in three different places.
const (
	codeIdentityRequired   = "IDENTITY_REQUIRED"
	codeIdentityUntrusted  = "IDENTITY_UNTRUSTED_PEER"
	codeIdentityMalformed  = "IDENTITY_MALFORMED"
	codeIdentityRoleDenied = "IDENTITY_ROLE_NOT_ALLOWED"
)

// requireIdentity wraps a handler so that the caller's identity is
// resolved and attached to the request context before the handler runs,
// and the request is refused if it cannot be.
//
// There is deliberately no fallback to the pipeline's own database
// role. Serving an unidentified caller as the service role is precisely
// the bypass this feature removes, and doing it as a fallback would
// reintroduce it in the one case nobody watches — the request that
// arrived without the header, because the proxy in front was
// misconfigured or bypassed. A refusal is noisy; a silent downgrade to
// full-corpus access is not.
//
// This is applied only to the query endpoint. The health, liveness,
// stats and discovery endpoints do not read a corpus, and requiring an
// identity for them would break ordinary probes to no benefit.
func (s *Server) requireIdentity(next http.Handler) http.Handler {
	if !s.config.Identity.Enabled {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.identityExtractor.Extract(r)
		if err != nil {
			status, code := classifyIdentityError(err)
			s.logger.Warn("refused request without a usable caller identity",
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"code", code,
				"error", err.Error())
			s.respondError(w, status, code, identityMessage(code, err))
			return
		}

		s.logger.Debug("request identity accepted",
			"path", r.URL.Path,
			"subject", id.Subject,
			"role", id.Role)

		next.ServeHTTP(w, r.WithContext(identity.NewContext(r.Context(), id)))
	})
}

// classifyIdentityError maps an extraction failure to an HTTP status
// and an error code.
//
// 401 is used for "you did not identify yourself" and 403 for "you did,
// and it is not acceptable from you", which is the ordinary reading of
// the two codes. Malformed claims are a 400: the request is broken
// rather than unauthorised, and the component that has to fix it is the
// proxy, not the caller.
func classifyIdentityError(err error) (int, string) {
	switch {
	case errors.Is(err, identity.ErrRequired):
		return http.StatusUnauthorized, codeIdentityRequired
	case errors.Is(err, identity.ErrUntrustedPeer):
		return http.StatusForbidden, codeIdentityUntrusted
	case errors.Is(err, identity.ErrMalformedClaims):
		return http.StatusBadRequest, codeIdentityMalformed
	case errors.Is(err, identity.ErrRoleNotAllowed):
		return http.StatusForbidden, codeIdentityRoleDenied
	default:
		// Extract returns only the errors above, so this is unreachable
		// today. It refuses rather than admits, because an unclassified
		// failure to establish an identity is still a failure to
		// establish an identity.
		return http.StatusUnauthorized, codeIdentityRequired
	}
}

// identityMessage returns the message sent to the caller.
//
// The underlying error text is included only for the cases whose detail
// is about the request itself and is already known to whoever sent it —
// which header was missing, which role was claimed. The untrusted-peer
// case gets a fixed message instead: its detail is the peer address and
// the configured CIDR blocks, which describe the deployment's network
// topology rather than the request, and there is no reason to hand that
// to a caller who has just failed a trust check. Full detail is logged
// either way.
func identityMessage(code string, err error) string {
	if code == codeIdentityUntrusted {
		return "requests from this address may not assert a caller identity"
	}
	return fmt.Sprintf("this server requires a caller identity: %s", err.Error())
}
