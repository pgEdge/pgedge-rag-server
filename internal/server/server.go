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
	"time"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/pipeline"
)

// PipelineManager defines the interface for pipeline management.
type PipelineManager interface {
	List() []pipeline.Info
	Get(name string) (*pipeline.Pipeline, error)
	Close() error
}

// Server is the HTTP server for the RAG API.
type Server struct {
	config    *config.Config
	pipelines PipelineManager
	logger    *slog.Logger
	server    *http.Server
	mux       *http.ServeMux
}

// New creates a new HTTP server.
func New(cfg *config.Config, pm PipelineManager, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		config:    cfg,
		pipelines: pm,
		logger:    logger,
		mux:       http.NewServeMux(),
	}

	// Set up routes
	s.setupRoutes()

	return s
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
