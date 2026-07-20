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
	"fmt"
	"net/http"

	"github.com/pgEdge/pgedge-rag-server/internal/pipeline"
)

// HealthResponse is the response for the health check endpoint.
type HealthResponse struct {
	Status    string                    `json:"status"`
	Pipelines []pipeline.PipelineHealth `json:"pipelines,omitempty"`
}

// PipelinesResponse is the response for the list pipelines endpoint.
type PipelinesResponse struct {
	Pipelines []pipeline.Info `json:"pipelines"`
}

// ErrorResponse is the standard error response format.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error information.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// maxRequestBodyBytes caps the size of a query request body. Generous
// enough for a query plus a long conversation history, small enough to
// reject clearly-oversized payloads before they reach the LLM/embedding
// call.
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// isRequestTimeout reports whether ctx's Done() channel closed because
// its deadline was exceeded (the server's own request timeout), as
// opposed to being canceled for another reason such as the client
// disconnecting.
func isRequestTimeout(ctx context.Context) bool {
	return errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// handleHealth handles the GET /health endpoint. It reports the server
// process as healthy unconditionally, and additionally pings every
// pipeline's LLM providers to surface connectivity problems in the
// response body — see issue #23. A provider being unreachable does
// not change the status code; it degrades "status" in the body so
// callers that only check for HTTP 200 keep working.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	pipelines := s.pipelines.Health(r.Context())

	status := "healthy"
	for _, p := range pipelines {
		if !p.Embedding.Reachable || !p.Completion.Reachable {
			status = "degraded"
			break
		}
	}

	s.respondJSON(w, http.StatusOK, HealthResponse{Status: status, Pipelines: pipelines})
}

// handleListPipelines handles the GET /pipelines endpoint.
func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	pipelines := s.pipelines.List()
	s.respondJSON(w, http.StatusOK, PipelinesResponse{Pipelines: pipelines})
}

// handlePipeline handles the POST /pipelines/{name} endpoint.
func (s *Server) handlePipeline(w http.ResponseWriter, r *http.Request) {
	// Extract pipeline name from URL path
	// Path format: /pipelines/{name}
	name := r.PathValue("name")
	if name == "" {
		s.respondError(w, http.StatusBadRequest, "INVALID_REQUEST", "pipeline name required")
		return
	}

	// Get the pipeline
	p, err := s.pipelines.Get(name)
	if err != nil {
		if errors.Is(err, pipeline.ErrPipelineNotFound) {
			s.respondError(w, http.StatusNotFound, "PIPELINE_NOT_FOUND",
				"pipeline not found: "+name)
			return
		}
		s.respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	// Parse request body first to validate input before checking pipeline
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	var req pipeline.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			s.respondError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE",
				fmt.Sprintf("request body exceeds maximum size of %d bytes", maxBytesErr.Limit))
			return
		}
		s.respondError(w, http.StatusBadRequest, "INVALID_REQUEST",
			"invalid request body: "+err.Error())
		return
	}

	if req.Query == "" {
		s.respondError(w, http.StatusBadRequest, "INVALID_REQUEST", "query is required")
		return
	}

	// Check for nil pipeline (shouldn't happen in production but good for safety)
	if p == nil {
		s.respondError(w, http.StatusInternalServerError, "INTERNAL_ERROR",
			"pipeline is nil")
		return
	}

	// Handle streaming vs non-streaming
	if req.Stream {
		s.handleStreamingQuery(w, r, p, req)
		return
	}

	// Execute non-streaming query, bounded so a hung upstream call (e.g.
	// a slow LLM API) gets a structured JSON timeout response instead of
	// running until the connection-level WriteTimeout kills it silently.
	ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
	defer cancel()

	resp, err := p.ExecuteWithOptions(ctx, req)
	if err != nil {
		if isRequestTimeout(ctx) {
			s.respondError(w, http.StatusGatewayTimeout, "REQUEST_TIMEOUT",
				"request took too long to process")
			return
		}
		s.logger.Error("pipeline execution failed",
			"pipeline", name,
			"error", err)
		s.respondError(w, http.StatusInternalServerError, "EXECUTION_ERROR", err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, resp)
}

// handleStreamingQuery handles a streaming RAG query using Server-Sent Events.
func (s *Server) handleStreamingQuery(w http.ResponseWriter, r *http.Request,
	p *pipeline.Pipeline, req pipeline.QueryRequest) {
	// Check if the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.respondError(w, http.StatusInternalServerError, "STREAMING_ERROR",
			"streaming not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Execute streaming query, bounded the same way as the non-streaming
	// path: a hung upstream call gets a structured SSE error event
	// instead of leaving the client waiting indefinitely. The response
	// status is already committed to 200 by the time streaming starts,
	// so the timeout can only be conveyed via the SSE stream itself, not
	// a different HTTP status code.
	ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
	defer cancel()

	chunkChan, errChan := p.ExecuteStreamWithOptions(ctx, req)

	// Stream chunks to client
	for {
		select {
		case chunk, ok := <-chunkChan:
			if !ok {
				// Channel closed, check for errors
				if err := <-errChan; err != nil {
					s.sendSSE(w, flusher, pipeline.StreamEvent{
						Type:  "error",
						Error: err.Error(),
					})
				}
				// Send done event
				s.sendSSE(w, flusher, pipeline.StreamEvent{
					Type: "done",
				})
				return
			}

			// Send chunk event
			event := pipeline.StreamEvent{
				Type:    "chunk",
				Content: chunk.Content,
			}
			s.sendSSE(w, flusher, event)

		case <-ctx.Done():
			if isRequestTimeout(ctx) {
				s.sendSSE(w, flusher, pipeline.StreamEvent{
					Type:  "error",
					Error: "request took too long to process",
				})
				s.sendSSE(w, flusher, pipeline.StreamEvent{Type: "done"})
				return
			}
			// Client disconnected
			s.logger.Debug("client disconnected during streaming")
			return
		}
	}
}

// sendSSE sends a Server-Sent Event.
func (s *Server) sendSSE(w http.ResponseWriter, flusher http.Flusher, event pipeline.StreamEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		s.logger.Error("failed to marshal SSE event", "error", err)
		return
	}

	// SSE format: data: {json}\n\n
	if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
		s.logger.Error("failed to write SSE event", "error", err)
		return
	}
	flusher.Flush()
}

// respondJSON sends a JSON response with RFC 8631 Link header for API discovery.
func (s *Server) respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	// RFC 8631: Link header for API documentation discovery
	w.Header().Set("Link", `</v1/openapi.json>; rel="service-desc"`)
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("failed to encode response", "error", err)
	}
}

// respondError sends an error response.
func (s *Server) respondError(w http.ResponseWriter, status int, code, message string) {
	s.respondJSON(w, status, ErrorResponse{
		Error: ErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}
