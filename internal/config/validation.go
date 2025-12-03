//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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
			c.Defaults.EmbeddingLLM, []string{"openai", "voyage", "ollama"})...)
	}

	// Validate RAG LLM if provider is specified
	if c.Defaults.RAGLLM.Provider != "" {
		errs = append(errs, c.validateLLMOptional("defaults.rag_llm",
			c.Defaults.RAGLLM, []string{"anthropic", "openai", "ollama"})...)
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
		[]string{"openai", "voyage", "ollama"})...)
	errs = append(errs, c.validateLLM(prefix+".rag_llm", p.RAGLLM,
		[]string{"anthropic", "openai", "ollama"})...)

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

	return errs
}

// validateDatabase validates database configuration.
func (c *Config) validateDatabase(prefix string, db DatabaseConfig) ValidationErrors {
	var errs ValidationErrors

	if db.Host == "" {
		errs = append(errs, ValidationError{
			Field:   prefix + ".host",
			Message: "required",
		})
	}

	if db.Database == "" {
		errs = append(errs, ValidationError{
			Field:   prefix + ".database",
			Message: "required",
		})
	}

	if db.Port < 1 || db.Port > 65535 {
		errs = append(errs, ValidationError{
			Field:   prefix + ".port",
			Message: "must be between 1 and 65535",
		})
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
