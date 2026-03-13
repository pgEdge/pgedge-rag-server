//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package database

import (
	"strings"
	"testing"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

func TestBuildConnectionString(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.DatabaseConfig
		contains    []string
		notContains []string
	}{
		{
			name: "legacy single host",
			cfg: config.DatabaseConfig{
				Host:     "h1",
				Port:     5432,
				Database: "db1",
			},
			contains: []string{
				"host=h1",
				"port=5432",
				"dbname=db1",
			},
		},
		{
			name: "multi-host basic",
			cfg: config.DatabaseConfig{
				Hosts: []config.HostEntry{
					{Host: "h1", Port: 5432},
					{Host: "h2", Port: 5432},
				},
				Database: "db1",
			},
			contains: []string{
				"host=h1,h2",
				"port=5432,5432",
				"dbname=db1",
			},
		},
		{
			name: "multi-host mixed ports",
			cfg: config.DatabaseConfig{
				Hosts: []config.HostEntry{
					{Host: "h1", Port: 5432},
					{Host: "h2", Port: 5433},
				},
				Database: "db1",
			},
			contains: []string{
				"host=h1,h2",
				"port=5432,5433",
			},
		},
		{
			name: "multi-host with target_session_attrs",
			cfg: config.DatabaseConfig{
				Hosts: []config.HostEntry{
					{Host: "h1", Port: 5432},
					{Host: "h2", Port: 5432},
				},
				TargetSessionAttrs: "prefer-standby",
				Database:           "db1",
			},
			contains: []string{
				"host=h1,h2",
				"port=5432,5432",
				"target_session_attrs=prefer-standby",
			},
		},
		{
			name: "legacy with no TSA",
			cfg: config.DatabaseConfig{
				Host:     "h1",
				Port:     5432,
				Database: "db1",
			},
			notContains: []string{
				"target_session_attrs",
			},
		},
		{
			name: "legacy with explicit TSA",
			cfg: config.DatabaseConfig{
				Host:               "h1",
				Port:               5432,
				TargetSessionAttrs: "any",
				Database:           "db1",
			},
			contains: []string{
				"target_session_attrs=any",
			},
		},
		{
			name: "three hosts",
			cfg: config.DatabaseConfig{
				Hosts: []config.HostEntry{
					{Host: "h1", Port: 5432},
					{Host: "h2", Port: 5432},
					{Host: "h3", Port: 5433},
				},
				Database: "db1",
			},
			contains: []string{
				"host=h1,h2,h3",
				"port=5432,5432,5433",
			},
		},
		{
			name: "with password and SSL",
			cfg: config.DatabaseConfig{
				Host:     "h1",
				Port:     5432,
				Database: "db1",
				Password: "secret",
				SSLMode:  "require",
			},
			contains: []string{
				"password=secret",
				"sslmode=require",
			},
		},
		{
			name: "with SSL certs",
			cfg: config.DatabaseConfig{
				Host:      "h1",
				Port:      5432,
				Database:  "db1",
				SSLMode:   "verify-full",
				SSLCert:   "/path/to/client.crt",
				SSLKey:    "/path/to/client.key",
				SSLRootCA: "/path/to/ca.crt",
			},
			contains: []string{
				"sslcert=/path/to/client.crt",
				"sslkey=/path/to/client.key",
				"sslrootcert=/path/to/ca.crt",
			},
		},
		{
			name: "with username",
			cfg: config.DatabaseConfig{
				Host:     "h1",
				Port:     5432,
				Database: "db1",
				Username: "myuser",
			},
			contains: []string{
				"user=myuser",
			},
		},
		{
			name: "IPv6 single host",
			cfg: config.DatabaseConfig{
				Host:     "::1",
				Port:     5432,
				Database: "db1",
			},
			contains: []string{
				"host=[::1]",
				"port=5432",
			},
		},
		{
			name: "IPv6 multi-host",
			cfg: config.DatabaseConfig{
				Hosts: []config.HostEntry{
					{Host: "2001:db8::1", Port: 5432},
					{Host: "2001:db8::2", Port: 5432},
				},
				TargetSessionAttrs: "prefer-standby",
				Database:           "db1",
			},
			contains: []string{
				"host=[2001:db8::1],[2001:db8::2]",
				"port=5432,5432",
			},
		},
		{
			name: "IPv6 already bracketed",
			cfg: config.DatabaseConfig{
				Host:     "[::1]",
				Port:     5432,
				Database: "db1",
			},
			contains: []string{
				"host=[::1]",
			},
			notContains: []string{
				"host=[[",
			},
		},
		{
			name: "mixed IPv4 and IPv6 multi-host",
			cfg: config.DatabaseConfig{
				Hosts: []config.HostEntry{
					{Host: "10.0.0.1", Port: 5432},
					{Host: "::1", Port: 5432},
					{Host: "pg-standby.example.com", Port: 5433},
				},
				Database: "db1",
			},
			contains: []string{
				"host=10.0.0.1,[::1],pg-standby.example.com",
				"port=5432,5432,5433",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For the username test, clear env vars that could interfere.
			if tt.name == "with username" {
				t.Setenv("PGUSER", "")
				t.Setenv("USER", "")
			}
			// For the "no TSA" and legacy tests where we check absence, also
			// clear env vars to keep DSN predictable.
			if tt.name == "legacy with no TSA" || tt.name == "legacy single host" {
				t.Setenv("PGUSER", "")
				t.Setenv("USER", "")
			}

			dsn := buildConnectionString(tt.cfg)

			for _, want := range tt.contains {
				if !strings.Contains(dsn, want) {
					t.Errorf("DSN %q does not contain %q", dsn, want)
				}
			}

			for _, notWant := range tt.notContains {
				if strings.Contains(dsn, notWant) {
					t.Errorf("DSN %q should not contain %q", dsn, notWant)
				}
			}
		})
	}
}
