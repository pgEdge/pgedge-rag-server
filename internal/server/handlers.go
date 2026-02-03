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
	"encoding/json"
	"errors"
	"net/http"

	"github.com/pgEdge/pgedge-rag-server/internal/pipeline"
)

// HealthResponse is the response for the health check endpoint.
type HealthResponse struct {
	Status string `json:"status"`
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

// handleHealth handles the GET /health endpoint.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondMethodNotAllowed(w, http.MethodGet)
		return
	}

	s.respondJSON(w, http.StatusOK, HealthResponse{Status: "healthy"})
}

// handleListPipelines handles the GET /pipelines endpoint.
func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondMethodNotAllowed(w, http.MethodGet)
		return
	}

	pipelines := s.pipelines.List()
	s.respondJSON(w, http.StatusOK, PipelinesResponse{Pipelines: pipelines})
}

// handlePipeline handles the POST /pipelines/{name} endpoint.
func (s *Server) handlePipeline(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondMethodNotAllowed(w, http.MethodPost)
		return
	}

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
	var req pipeline.QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	// Execute non-streaming query
	resp, err := p.ExecuteWithOptions(r.Context(), req)
	if err != nil {
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

	// Execute streaming query
	chunkChan, errChan := p.ExecuteStreamWithOptions(r.Context(), req)

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

		case <-r.Context().Done():
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

// respondMethodNotAllowed sends a 405 Method Not Allowed response.
func (s *Server) respondMethodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	s.respondError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED",
		"method not allowed")
}
