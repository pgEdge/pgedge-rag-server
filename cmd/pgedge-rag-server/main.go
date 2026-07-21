//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
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
	"github.com/pgEdge/pgedge-rag-server/internal/watch"
)

// Version information - set via ldflags during build
var (
	version   = "1.0.0"
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

// pipelineCloseGracePeriod is how long a swapped-out pipeline manager is
// kept alive after a hot-reload before its database and LLM clients are
// closed, so requests still using it can finish first. It sits above the
// server's maximum request lifetime (a request is bounded by
// server.DefaultRequestTimeout) plus a small margin for the response to
// flush, so an in-flight request cannot outlive the manager it started
// on — see issue #30.
const pipelineCloseGracePeriod = server.DefaultRequestTimeout + 10*time.Second

func run(configPath string, logger *slog.Logger) error {
	// Resolve the config file path up front so it can also be watched
	// for changes (config.Load re-resolves it internally too, but that's
	// cheap and keeps this function simple).
	resolvedConfigPath, err := config.FindConfigFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to locate configuration file: %w", err)
	}

	cfg, err := config.Load(resolvedConfigPath)
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

	// Create and start server
	srv := server.New(cfg, pm, logger)

	// Close whatever pipeline manager is active at shutdown time, not
	// necessarily the one created above — a reload may have swapped it
	// out for a newer one in the meantime.
	defer func() {
		if current := srv.SwapPipelineManager(nil); current != nil {
			if err := current.Close(); err != nil {
				logger.Error("failed to close pipeline manager", "error", err)
			}
		}
	}()

	// Watch the config file and any file-based API keys it uses (e.g. a
	// mounted secret) for changes, and reload without a restart when
	// they change — see issue #30.
	watchPaths := append([]string{resolvedConfigPath}, config.APIKeyFilePaths(cfg)...)
	reload := func() {
		logger.Info("configuration change detected, reloading")

		newCfg, err := config.Load(resolvedConfigPath)
		if err != nil {
			logger.Error("config reload failed; keeping previous configuration", "error", err)
			return
		}

		newPM, err := pipeline.NewManagerWithLogger(pipeline.ManagerConfig{
			Config: newCfg,
			Logger: logger,
		})
		if err != nil {
			logger.Error("pipeline reload failed; keeping previous configuration", "error", err)
			return
		}

		oldPM := srv.SwapPipelineManager(newPM)
		logger.Info("configuration reloaded", "pipelines", len(newCfg.Pipelines))

		if oldPM != nil {
			// Give in-flight requests using the old manager time to finish
			// before closing its DB connections/LLM clients. The delay
			// must exceed the server's maximum request lifetime so a query
			// that started just before the swap cannot still be using the
			// old manager when it's closed; deriving it from
			// server.DefaultRequestTimeout keeps the two in step if that
			// bound ever changes.
			time.AfterFunc(pipelineCloseGracePeriod, func() {
				if err := oldPM.Close(); err != nil {
					logger.Warn("failed to close previous pipeline manager after reload", "error", err)
				}
			})
		}
	}

	fileWatcher, err := watch.New(watchPaths, watch.DefaultDebounce, reload, logger)
	if err != nil {
		logger.Warn("failed to start configuration watcher; hot-reload disabled", "error", err)
	} else {
		watchCtx, cancelWatch := context.WithCancel(context.Background())
		defer cancelWatch()
		go fileWatcher.Start(watchCtx)
		defer fileWatcher.Close()
		logger.Info("watching for configuration changes", "paths", watchPaths)
	}

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
