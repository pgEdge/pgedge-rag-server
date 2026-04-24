//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Copyright (c) 2025 - 2026, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxPipelineNameLen is the maximum allowed length for a pipeline name.
const maxPipelineNameLen = 63

// pipelineNameRe is the allowlist pattern for pipeline names.
// Only lowercase letters, digits, hyphens, and underscores are permitted.
var pipelineNameRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

// expandPath expands ~ to the user's home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// ValidationError represents a single configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}

	msgs := make([]string, 0, len(e))
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// Validate checks the configuration for errors and returns all validation
// errors found.
func (c *Config) Validate() error {
	var errs ValidationErrors

	// Validate server config
	errs = append(errs, c.validateServer()...)

	// Validate defaults
	errs = append(errs, c.validateDefaults()...)

	// Validate pipelines
	errs = append(errs, c.validatePipelines()...)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// validateServer validates server configuration.
func (c *Config) validateServer() ValidationErrors {
	var errs ValidationErrors

	if c.Server.Port < 1 || c.Server.Port > 65535 {
		errs = append(errs, ValidationError{
			Field:   "server.port",
			Message: "must be between 1 and 65535",
		})
	}

	if c.Server.TLS.Enabled {
		if c.Server.TLS.CertFile == "" {
			errs = append(errs, ValidationError{
				Field:   "server.tls.cert_file",
				Message: "required when TLS is enabled",
			})
		} else if _, err := os.Stat(expandPath(c.Server.TLS.CertFile)); err != nil {
			errs = append(errs, ValidationError{
				Field:   "server.tls.cert_file",
				Message: fmt.Sprintf("file not found: %s", c.Server.TLS.CertFile),
			})
		}

		if c.Server.TLS.KeyFile == "" {
			errs = append(errs, ValidationError{
				Field:   "server.tls.key_file",
				Message: "required when TLS is enabled",
			})
		} else if _, err := os.Stat(expandPath(c.Server.TLS.KeyFile)); err != nil {
			errs = append(errs, ValidationError{
				Field:   "server.tls.key_file",
				Message: fmt.Sprintf("file not found: %s", c.Server.TLS.KeyFile),
			})
		}
	}

	return errs
}

// validateDefaults validates the defaults configuration.
func (c *Config) validateDefaults() ValidationErrors {
	var errs ValidationErrors

	// Validate embedding LLM if provider is specified
	if c.Defaults.EmbeddingLLM.Provider != "" {
		errs = append(errs, c.validateLLMOptional("defaults.embedding_llm",
			c.Defaults.EmbeddingLLM, []string{"openai", "voyage", "ollama", "gemini"})...)
	}

	// Validate RAG LLM if provider is specified
	if c.Defaults.RAGLLM.Provider != "" {
		errs = append(errs, c.validateLLMOptional("defaults.rag_llm",
			c.Defaults.RAGLLM, []string{"anthropic", "openai", "ollama", "gemini"})...)
	}

	return errs
}

// validatePipelines validates all pipeline configurations.
func (c *Config) validatePipelines() ValidationErrors {
	var errs ValidationErrors

	if len(c.Pipelines) == 0 {
		errs = append(errs, ValidationError{
			Field:   "pipelines",
			Message: "at least one pipeline must be configured",
		})
		return errs
	}

	// Check for duplicate pipeline names
	names := make(map[string]bool)
	for i, p := range c.Pipelines {
		if names[p.Name] {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("pipelines[%d].name", i),
				Message: fmt.Sprintf("duplicate pipeline name: %s", p.Name),
			})
		}
		names[p.Name] = true

		errs = append(errs, c.validatePipeline(i, p)...)
	}

	return errs
}

// validatePipeline validates a single pipeline configuration.
func (c *Config) validatePipeline(index int, p Pipeline) ValidationErrors {
	var errs ValidationErrors
	prefix := fmt.Sprintf("pipelines[%d]", index)

	// Required fields
	if p.Name == "" {
		errs = append(errs, ValidationError{
			Field:   prefix + ".name",
			Message: "required",
		})
	} else if len(p.Name) > maxPipelineNameLen {
		errs = append(errs, ValidationError{
			Field:   prefix + ".name",
			Message: fmt.Sprintf("must be %d characters or fewer", maxPipelineNameLen),
		})
	} else if !pipelineNameRe.MatchString(p.Name) {
		errs = append(errs, ValidationError{
			Field:   prefix + ".name",
			Message: "must contain only lowercase letters, digits, hyphens, and underscores (^[a-z0-9_-]+$)",
		})
	}

	// Database validation
	errs = append(errs, c.validateDatabase(prefix+".database", p.Database)...)

	// Tables validation
	if len(p.Tables) == 0 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".tables",
			Message: "at least one table must be configured",
		})
	} else {
		for j, ts := range p.Tables {
			errs = append(errs, c.validateTable(
				fmt.Sprintf("%s.tables[%d]", prefix, j), ts)...)
		}
	}

	// LLM validation
	errs = append(errs, c.validateLLM(prefix+".embedding_llm", p.EmbeddingLLM,
		[]string{"openai", "voyage", "ollama", "gemini"})...)
	errs = append(errs, c.validateLLM(prefix+".rag_llm", p.RAGLLM,
		[]string{"anthropic", "openai", "ollama", "gemini"})...)

	// Token budget validation
	if p.TokenBudget < 0 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".token_budget",
			Message: "must be non-negative",
		})
	}

	// Top N validation
	if p.TopN < 0 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".top_n",
			Message: "must be non-negative",
		})
	}

	// Search config validation
	if p.Search.VectorWeight != nil {
		w := *p.Search.VectorWeight
		if w < 0.0 || w > 1.0 {
			errs = append(errs, ValidationError{
				Field:   prefix + ".search.vector_weight",
				Message: "must be between 0.0 and 1.0",
			})
		}
	}

	if p.Search.MinSimilarity != nil {
		ms := *p.Search.MinSimilarity
		if ms < 0.0 || ms > 1.0 {
			errs = append(errs, ValidationError{
				Field:   prefix + ".search.min_similarity",
				Message: "must be between 0.0 and 1.0",
			})
		}
	}

	return errs
}

// validateDatabase validates database configuration.
func (c *Config) validateDatabase(prefix string, db DatabaseConfig) ValidationErrors {
	var errs ValidationErrors

	// Mutual exclusion: hosts and host cannot both be set
	if len(db.Hosts) > 0 && db.Host != "" {
		errs = append(errs, ValidationError{
			Field:   prefix,
			Message: "cannot specify both 'hosts' and 'host'; use one or the other",
		})
		return errs
	}

	if len(db.Hosts) > 0 {
		// Multi-host validation
		for i, h := range db.Hosts {
			entryPrefix := fmt.Sprintf("%s.hosts[%d]", prefix, i)
			if h.Host == "" {
				errs = append(errs, ValidationError{
					Field:   entryPrefix + ".host",
					Message: "required",
				})
			} else {
				errs = append(errs, validateHostValue(entryPrefix+".host", h.Host)...)
			}
			if h.Port < 1 || h.Port > 65535 {
				errs = append(errs, ValidationError{
					Field:   entryPrefix + ".port",
					Message: "must be between 1 and 65535",
				})
			}
		}
	} else {
		// Legacy single-host validation
		if db.Host == "" {
			errs = append(errs, ValidationError{
				Field:   prefix + ".host",
				Message: "required",
			})
		} else {
			errs = append(errs, validateHostValue(prefix+".host", db.Host)...)
		}

		if db.Port < 1 || db.Port > 65535 {
			errs = append(errs, ValidationError{
				Field:   prefix + ".port",
				Message: "must be between 1 and 65535",
			})
		}
	}

	if db.Database == "" {
		errs = append(errs, ValidationError{
			Field:   prefix + ".database",
			Message: "required",
		})
	}

	// Validate target_session_attrs
	if db.TargetSessionAttrs != "" {
		if len(db.Hosts) == 0 {
			errs = append(errs, ValidationError{
				Field:   prefix + ".target_session_attrs",
				Message: "only supported with multi-host 'hosts' configuration",
			})
		} else {
			validTSA := map[string]bool{
				"any":            true,
				"read-write":     true,
				"read-only":      true,
				"primary":        true,
				"standby":        true,
				"prefer-standby": true,
			}
			if !validTSA[db.TargetSessionAttrs] {
				errs = append(errs, ValidationError{
					Field:   prefix + ".target_session_attrs",
					Message: "must be one of: any, read-write, read-only, primary, standby, prefer-standby",
				})
			}
		}
	}

	// Validate SSL mode
	validSSLModes := map[string]bool{
		"disable":     true,
		"allow":       true,
		"prefer":      true,
		"require":     true,
		"verify-ca":   true,
		"verify-full": true,
	}
	if db.SSLMode != "" && !validSSLModes[db.SSLMode] {
		errs = append(errs, ValidationError{
			Field:   prefix + ".ssl_mode",
			Message: "must be one of: disable, allow, prefer, require, verify-ca, verify-full",
		})
	}

	return errs
}

// validateTable validates a table source configuration.
func (c *Config) validateTable(prefix string, ts TableSource) ValidationErrors {
	var errs ValidationErrors

	if ts.Table == "" {
		errs = append(errs, ValidationError{
			Field:   prefix + ".table",
			Message: "required",
		})
	}

	if ts.TextColumn == "" {
		errs = append(errs, ValidationError{
			Field:   prefix + ".text_column",
			Message: "required",
		})
	}

	if ts.VectorColumn == "" {
		errs = append(errs, ValidationError{
			Field:   prefix + ".vector_column",
			Message: "required",
		})
	}

	return errs
}

// validateLLM validates LLM configuration (required fields).
func (c *Config) validateLLM(prefix string, llm LLMConfig, validProviders []string) ValidationErrors {
	var errs ValidationErrors

	if llm.Provider == "" {
		errs = append(errs, ValidationError{
			Field:   prefix + ".provider",
			Message: "required",
		})
	} else {
		provider := strings.ToLower(llm.Provider)
		valid := false
		for _, vp := range validProviders {
			if provider == vp {
				valid = true
				break
			}
		}
		if !valid {
			errs = append(errs, ValidationError{
				Field:   prefix + ".provider",
				Message: fmt.Sprintf("must be one of: %s", strings.Join(validProviders, ", ")),
			})
		}
	}

	if llm.Model == "" {
		errs = append(errs, ValidationError{
			Field:   prefix + ".model",
			Message: "required",
		})
	}

	return errs
}

// validateLLMOptional validates LLM configuration when provider is set.
// Unlike validateLLM, this doesn't require provider/model to be present,
// but validates them if they are.
func (c *Config) validateLLMOptional(prefix string, llm LLMConfig, validProviders []string) ValidationErrors {
	var errs ValidationErrors

	// Validate provider if set
	if llm.Provider != "" {
		provider := strings.ToLower(llm.Provider)
		valid := false
		for _, vp := range validProviders {
			if provider == vp {
				valid = true
				break
			}
		}
		if !valid {
			errs = append(errs, ValidationError{
				Field:   prefix + ".provider",
				Message: fmt.Sprintf("must be one of: %s", strings.Join(validProviders, ", ")),
			})
		}

		// Model is required when provider is set
		if llm.Model == "" {
			errs = append(errs, ValidationError{
				Field:   prefix + ".model",
				Message: "required when provider is set",
			})
		}
	}

	return errs
}

// validateHostValue validates a single host string, accepting hostnames,
// IPv4 addresses, and IPv6 addresses (optionally bracketed).
func validateHostValue(field string, host string) ValidationErrors {
	var errs ValidationErrors

	// Strip brackets for IPv6 validation: [::1] → ::1
	bare := host
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		bare = host[1 : len(host)-1]
		// Bracketed values must be valid IPv6 addresses
		if bare == "" || net.ParseIP(bare) == nil {
			errs = append(errs, ValidationError{
				Field:   field,
				Message: "invalid IPv6 address",
			})
		}
		return errs
	}

	// If it contains a colon it must be a valid IPv6 address
	if strings.Contains(bare, ":") {
		if net.ParseIP(bare) == nil {
			errs = append(errs, ValidationError{
				Field:   field,
				Message: "invalid IPv6 address",
			})
		}
		return errs
	}

	// Hostname / IPv4: reject unsafe characters
	if strings.ContainsAny(bare, ", \t\n\r@/?='\\#") {
		errs = append(errs, ValidationError{
			Field:   field,
			Message: "must not contain commas, whitespace, @, /, ?, =, ', \\, or #",
		})
	}

	return errs
}
