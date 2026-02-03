//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

// Package database provides PostgreSQL connectivity and vector search.
package database

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

// Pool wraps a pgxpool connection pool.
type Pool struct {
	pool   *pgxpool.Pool
	config config.DatabaseConfig
}

// NewPool creates a new database connection pool.
func NewPool(ctx context.Context, cfg config.DatabaseConfig) (*Pool, error) {
	connStr := buildConnectionString(cfg)

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Pool{
		pool:   pool,
		config: cfg,
	}, nil
}

// buildConnectionString constructs a PostgreSQL connection string.
func buildConnectionString(cfg config.DatabaseConfig) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("host=%s", cfg.Host))
	parts = append(parts, fmt.Sprintf("port=%d", cfg.Port))
	parts = append(parts, fmt.Sprintf("dbname=%s", cfg.Database))

	// Username: config > PGUSER > USER
	username := cfg.Username
	if username == "" {
		username = os.Getenv("PGUSER")
	}
	if username == "" {
		username = os.Getenv("USER")
	}
	if username != "" {
		parts = append(parts, fmt.Sprintf("user=%s", username))
	}

	if cfg.Password != "" {
		parts = append(parts, fmt.Sprintf("password=%s", cfg.Password))
	}

	if cfg.SSLMode != "" {
		parts = append(parts, fmt.Sprintf("sslmode=%s", cfg.SSLMode))
	}

	// Certificate-based authentication
	if cfg.SSLCert != "" {
		parts = append(parts, fmt.Sprintf("sslcert=%s", cfg.SSLCert))
	}
	if cfg.SSLKey != "" {
		parts = append(parts, fmt.Sprintf("sslkey=%s", cfg.SSLKey))
	}
	if cfg.SSLRootCA != "" {
		parts = append(parts, fmt.Sprintf("sslrootcert=%s", cfg.SSLRootCA))
	}

	return strings.Join(parts, " ")
}

// Ping verifies the database connection.
func (p *Pool) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// Close closes the connection pool.
func (p *Pool) Close() {
	if p.pool != nil {
		p.pool.Close()
	}
}

// Pool returns the underlying pgxpool.Pool for direct access.
func (p *Pool) Pool() *pgxpool.Pool {
	return p.pool
}
