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
	pool     *pgxpool.Pool
	config   config.DatabaseConfig
	identity config.IdentityConfig
}

// NewPool creates a new database connection pool.
//
// idCfg decides whether queries run as the caller who made the request
// or as the role in cfg. It is taken here, at pool construction, rather
// than passed per query, because it governs how every query on this
// pool is issued — see Pool.withRows.
func NewPool(
	ctx context.Context,
	cfg config.DatabaseConfig,
	idCfg config.IdentityConfig,
) (*Pool, error) {
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
		pool:     pool,
		config:   cfg,
		identity: idCfg.WithDefaults(),
	}, nil
}

// buildConnectionString constructs a PostgreSQL keyword/value
// connection string. Every value goes through connParam, so passwords,
// paths and names containing spaces, quotes or backslashes reach the
// driver unchanged.
func buildConnectionString(cfg config.DatabaseConfig) string {
	var parts []string

	if len(cfg.Hosts) > 0 {
		// Multi-host DSN: host=h1,h2,h3 port=p1,p2,p3
		hosts := make([]string, len(cfg.Hosts))
		ports := make([]string, len(cfg.Hosts))
		for i, h := range cfg.Hosts {
			hosts[i] = bracketIPv6(h.Host)
			ports[i] = fmt.Sprintf("%d", h.Port)
		}
		parts = append(parts, connParam("host", strings.Join(hosts, ",")))
		parts = append(parts, connParam("port", strings.Join(ports, ",")))
	} else {
		// Legacy single-host DSN
		parts = append(parts, connParam("host", bracketIPv6(cfg.Host)))
		parts = append(parts, connParam("port", fmt.Sprintf("%d", cfg.Port)))
	}

	parts = append(parts, connParam("dbname", cfg.Database))

	// Username: config > PGUSER > USER
	username := cfg.Username
	if username == "" {
		username = os.Getenv("PGUSER")
	}
	if username == "" {
		username = os.Getenv("USER")
	}
	if username != "" {
		parts = append(parts, connParam("user", username))
	}

	if cfg.Password != "" {
		parts = append(parts, connParam("password", cfg.Password))
	}

	if cfg.SSLMode != "" {
		parts = append(parts, connParam("sslmode", cfg.SSLMode))
	}

	// Certificate-based authentication
	if cfg.SSLCert != "" {
		parts = append(parts, connParam("sslcert", cfg.SSLCert))
	}
	if cfg.SSLKey != "" {
		parts = append(parts, connParam("sslkey", cfg.SSLKey))
	}
	if cfg.SSLRootCA != "" {
		parts = append(parts, connParam("sslrootcert", cfg.SSLRootCA))
	}

	// Multi-host routing
	if cfg.TargetSessionAttrs != "" {
		parts = append(parts, connParam("target_session_attrs", cfg.TargetSessionAttrs))
	}

	return strings.Join(parts, " ")
}

// connParam formats one keyword/value pair using libpq's quoting rules:
// a backslash escapes the character after it, and a value that is
// empty or contains whitespace must be single-quoted. pgx follows the
// same rules from v5.11.0 onwards, so an unescaped backslash in a
// password would otherwise be silently dropped. Values that need no
// escaping are written bare, so ordinary connection strings read as
// they always have.
func connParam(key, value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)
	if value == "" || strings.ContainsAny(value, " \t\n\r\v\f'\\") {
		return key + "='" + escaped + "'"
	}
	return key + "=" + escaped
}

// bracketIPv6 wraps an IPv6 address in square brackets for libpq.
// Hostnames and IPv4 addresses are returned unchanged.
func bracketIPv6(host string) string {
	if strings.HasPrefix(host, "[") {
		return host // already bracketed
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
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
