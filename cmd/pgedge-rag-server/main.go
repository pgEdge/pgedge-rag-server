//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
	"github.com/pgEdge/pgedge-rag-server/internal/pipeline"
	"github.com/pgEdge/pgedge-rag-server/internal/server"
)

// Version information - set via ldflags during build
var (
	version   = "1.0.0-alpha2"
	buildTime = "unknown"
	gitCommit = "unknown"
)

func main() {
	var (
		showVersion = flag.Bool("version", false, "Show version information")
		showHelp    = flag.Bool("help", false, "Show help message")
		showOpenAPI = flag.Bool("openapi", false, "Output OpenAPI specification and exit")
		configPath  = flag.String("config", "", "Path to configuration file")
	)

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `pgEdge RAG Server - Retrieval-Augmented Generation for PostgreSQL

Usage:
    pgedge-rag-server [options]

Options:
    -config string
        Path to configuration file. If not specified, searches:
        1. /etc/pgedge/pgedge-rag-server.yaml
        2. pgedge-rag-server.yaml (in binary directory)

    -openapi
        Output OpenAPI v3 specification as JSON and exit

    -version
        Show version information and exit

    -help
        Show this help message and exit

For more information, visit: https://github.com/pgEdge/pgedge-rag-server
`)
	}

	flag.Parse()

	if *showHelp {
		flag.Usage()
		os.Exit(0)
	}

	if *showVersion {
		fmt.Printf("pgEdge RAG Server\n")
		fmt.Printf("  Version:    %s\n", version)
		fmt.Printf("  Build Time: %s\n", buildTime)
		fmt.Printf("  Git Commit: %s\n", gitCommit)
		os.Exit(0)
	}

	if *showOpenAPI {
		spec := server.BuildOpenAPISpec()
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(spec); err != nil {
			fmt.Fprintf(os.Stderr, "failed to encode OpenAPI spec: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Set up logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Run the server
	if err := run(*configPath, logger); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run(configPath string, logger *slog.Logger) error {
	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	logger.Info("configuration loaded",
		"pipelines", len(cfg.Pipelines))

	// Create pipeline manager
	pm, err := pipeline.NewManagerWithLogger(pipeline.ManagerConfig{
		Config: cfg,
		Logger: logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create pipeline manager: %w", err)
	}
	defer func() {
		if err := pm.Close(); err != nil {
			logger.Error("failed to close pipeline manager", "error", err)
		}
	}()

	// Create and start server
	srv := server.New(cfg, pm, logger)

	// Handle graceful shutdown
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	// Wait for shutdown signal or server error
	select {
	case err := <-errCh:
		return err
	case sig := <-shutdownCh:
		logger.Info("received shutdown signal", "signal", sig)

		// Give 30 seconds for graceful shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		return srv.Shutdown(ctx)
	}
}
