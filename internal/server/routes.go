//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package server

// setupRoutes configures all HTTP routes.
func (s *Server) setupRoutes() {
	// API v1 routes
	s.mux.HandleFunc("GET /v1/openapi.json", s.handleOpenAPI)
	s.mux.HandleFunc("GET /v1/health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/pipelines", s.handleListPipelines)
	s.mux.HandleFunc("POST /v1/pipelines/{name}", s.handlePipeline)
}
