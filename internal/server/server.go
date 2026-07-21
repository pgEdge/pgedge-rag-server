//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package server provides the HTTP server for the RAG API.
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/pipeline"
)

// PipelineManager defines the interface for pipeline management.
type PipelineManager interface {
	List() []pipeline.Info

	// GetExecutor retrieves a pipeline by name as the narrow
	// QueryExecutor interface, so the server depends only on the
	// ability to run a query, not on *pipeline.Pipeline directly —
	// this is what lets tests inject a fake that hangs, errors, or
	// returns a controlled result. See issue #37.
	GetExecutor(name string) (pipeline.QueryExecutor, error)

	Stats() []pipeline.Usage
	Health(ctx context.Context) []pipeline.PipelineHealth
	Close() error
}

// DefaultRequestTimeout bounds how long a single pipeline query may run
// (embedding + search + LLM call) before the server gives up and returns
// a structured JSON timeout error. Kept comfortably below WriteTimeout so
// there's time left to write that response before the connection-level
// timeout would otherwise kill the connection with no body at all — see
// issue #31. Exported so callers coordinating with the request lifetime
// (e.g. how long a swapped-out pipeline manager must stay alive during a
// hot-reload) can reference it rather than duplicating the value.
const DefaultRequestTimeout = 50 * time.Second

// Server is the HTTP server for the RAG API.
type Server struct {
	config         *config.Config
	logger         *slog.Logger
	server         *http.Server
	mux            *http.ServeMux
	pipelinesMu    sync.RWMutex
	pipelines      PipelineManager // guarded by pipelinesMu; use pipelineManager()/SwapPipelineManager
	requestTimeout time.Duration
}

// New creates a new HTTP server.
func New(cfg *config.Config, pm PipelineManager, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		config:         cfg,
		pipelines:      pm,
		logger:         logger,
		mux:            http.NewServeMux(),
		requestTimeout: DefaultRequestTimeout,
	}

	// Set up routes
	s.setupRoutes()

	return s
}

// pipelineManager returns the currently active PipelineManager. Safe for
// concurrent use with SwapPipelineManager.
func (s *Server) pipelineManager() PipelineManager {
	s.pipelinesMu.RLock()
	defer s.pipelinesMu.RUnlock()
	return s.pipelines
}

// SwapPipelineManager atomically replaces the active PipelineManager and
// returns the one it replaced, so the caller can close it once any
// in-flight requests still using it have had a chance to finish — see
// issue #30 (config/secret hot-reload).
func (s *Server) SwapPipelineManager(pm PipelineManager) PipelineManager {
	s.pipelinesMu.Lock()
	defer s.pipelinesMu.Unlock()
	old := s.pipelines
	s.pipelines = pm
	return old
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("%s:%d", s.config.Server.ListenAddress, s.config.Server.Port)

	s.server = &http.Server{
		Addr:         addr,
		Handler:      s.applyMiddleware(s.mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	s.logger.Info("starting server",
		"address", addr,
		"tls", s.config.Server.TLS.Enabled)

	if s.config.Server.TLS.Enabled {
		return s.serveTLS()
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	return s.server.Serve(listener)
}

// serveTLS starts the server with TLS.
func (s *Server) serveTLS() error {
	tlsCfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	s.server.TLSConfig = tlsCfg

	return s.server.ListenAndServeTLS(
		s.config.Server.TLS.CertFile,
		s.config.Server.TLS.KeyFile,
	)
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down server")

	if s.server != nil {
		return s.server.Shutdown(ctx)
	}

	return nil
}

// Addr returns the server's address. Returns empty string if not started.
func (s *Server) Addr() string {
	if s.server != nil {
		return s.server.Addr
	}
	return ""
}
