//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package server

import (
	"net/http"
	"runtime/debug"
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
	handler = s.loggingMiddleware(handler)
	handler = s.recoveryMiddleware(handler)
	if s.config.Server.CORS.Enabled {
		handler = s.corsMiddleware(handler)
	}
	return handler
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
