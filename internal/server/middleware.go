//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
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
