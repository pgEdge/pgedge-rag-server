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
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher to support SSE streaming.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// applyMiddleware wraps the handler with all middleware.
func (s *Server) applyMiddleware(handler http.Handler) http.Handler {
	// Apply in reverse order (last applied runs first)
	handler = s.routingMiddleware(handler)
	handler = s.loggingMiddleware(handler)
	handler = s.recoveryMiddleware(handler)
	if s.config.Server.CORS.Enabled {
		handler = s.corsMiddleware(handler)
	}
	return handler
}

// routingMiddleware intercepts requests that don't match any registered
// route and returns a structured JSON error instead of net/http's default
// plain-text response. http.ServeMux has no way to customize its built-in
// "404 page not found" / "405 Method Not Allowed" handlers directly, so
// this checks the match itself via mux.Handler, which returns an empty
// pattern both when no route matches the path at all and when the path
// matches but the method doesn't. Distinguishing those two cases (to
// return 404 vs 405 with a correct Allow header) requires probing the
// mux for every candidate method, since ServeMux doesn't expose a way to
// list which methods a path supports either.
//
// This only runs when the mux itself found no match, so it never
// touches a deliberate application-level response (e.g. handlePipeline's
// PIPELINE_NOT_FOUND 404) — those only happen after a route has already
// matched and dispatched to a handler.
func (s *Server) routingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := s.mux.Handler(r); pattern == "" {
			if allowed := s.allowedMethods(r); len(allowed) > 0 {
				w.Header().Set("Allow", strings.Join(allowed, ", "))
				s.respondError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
					"method not allowed")
				return
			}
			s.respondError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allowedMethods returns which of the server's supported HTTP methods
// have a registered route matching r's path, by probing the mux directly.
// ServeMux doesn't expose a public API to list this, so each candidate
// method is checked by cloning the request with that method substituted.
func (s *Server) allowedMethods(r *http.Request) []string {
	methods := []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete,
	}

	var allowed []string
	for _, method := range methods {
		probe := r.Clone(r.Context())
		probe.Method = method
		if _, pattern := s.mux.Handler(probe); pattern != "" {
			allowed = append(allowed, method)
			// net/http.ServeMux implicitly serves HEAD for any
			// GET-registered pattern, so a route supporting GET also
			// supports HEAD even though only GET is registered.
			if method == http.MethodGet {
				allowed = append(allowed, http.MethodHead)
			}
		}
	}
	return allowed
}

// loggingMiddleware logs request information.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		s.logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration", time.Since(start).String(),
			"remote", r.RemoteAddr)
	})
}

// recoveryMiddleware recovers from panics and returns 500.
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic recovered",
					"error", rec,
					"stack", string(debug.Stack()))

				s.respondError(w, http.StatusInternalServerError,
					"INTERNAL_ERROR", "internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// corsMiddleware adds CORS headers and handles preflight requests.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		allowedOrigin := s.getAllowedOrigin(origin)

		if allowedOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// getAllowedOrigin checks if the request origin is allowed.
// Returns the allowed origin or empty string if not allowed.
func (s *Server) getAllowedOrigin(origin string) string {
	if origin == "" {
		return ""
	}

	allowedOrigins := s.config.Server.CORS.AllowedOrigins

	// If no origins configured, allow none
	if len(allowedOrigins) == 0 {
		return ""
	}

	// Check for wildcard
	for _, allowed := range allowedOrigins {
		if allowed == "*" {
			return "*"
		}
		if allowed == origin {
			return origin
		}
	}

	return ""
}
