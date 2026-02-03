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

func TestBuildFilterClause(t *testing.T) {
	tests := []struct {
		name          string
		configFilter  *config.ConfigFilter
		requestFilter *config.Filter
		expectedSQL   string
		expectedArgs  []interface{}
		expectError   bool
	}{
		{
			name:         "no filters",
			expectedSQL:  "",
			expectedArgs: nil,
		},
		{
			name: "simple equality",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "product", Operator: "=", Value: "pgAdmin"},
				},
			},
			expectedSQL:  " WHERE (\"product\" = $1)",
			expectedArgs: []interface{}{"pgAdmin"},
		},
		{
			name: "multiple conditions AND",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "product", Operator: "=", Value: "pgAdmin"},
					{Column: "version", Operator: ">=", Value: "v8.0"},
				},
				Logic: "AND",
			},
			expectedSQL:  " WHERE (\"product\" = $1 AND \"version\" >= $2)",
			expectedArgs: []interface{}{"pgAdmin", "v8.0"},
		},
		{
			name: "multiple conditions OR",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "status", Operator: "=", Value: "published"},
					{Column: "status", Operator: "=", Value: "draft"},
				},
				Logic: "OR",
			},
			expectedSQL:  " WHERE (\"status\" = $1 OR \"status\" = $2)",
			expectedArgs: []interface{}{"published", "draft"},
		},
		{
			name: "IN operator",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "status", Operator: "IN", Value: []interface{}{"published", "draft"}},
				},
			},
			expectedSQL:  " WHERE (\"status\" IN ($1, $2))",
			expectedArgs: []interface{}{"published", "draft"},
		},
		{
			name: "NOT IN operator",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "status", Operator: "NOT IN", Value: []interface{}{"archived", "deleted"}},
				},
			},
			expectedSQL:  " WHERE (\"status\" NOT IN ($1, $2))",
			expectedArgs: []interface{}{"archived", "deleted"},
		},
		{
			name: "IS NULL operator",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "deleted_at", Operator: "IS NULL"},
				},
			},
			expectedSQL:  " WHERE (\"deleted_at\" IS NULL)",
			expectedArgs: nil,
		},
		{
			name: "IS NOT NULL operator",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "published_at", Operator: "IS NOT NULL"},
				},
			},
			expectedSQL:  " WHERE (\"published_at\" IS NOT NULL)",
			expectedArgs: nil,
		},
		{
			name: "LIKE operator",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "title", Operator: "LIKE", Value: "%backup%"},
				},
			},
			expectedSQL:  " WHERE (\"title\" LIKE $1)",
			expectedArgs: []interface{}{"%backup%"},
		},
		{
			name: "ILIKE operator",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "title", Operator: "ILIKE", Value: "%backup%"},
				},
			},
			expectedSQL:  " WHERE (\"title\" ILIKE $1)",
			expectedArgs: []interface{}{"%backup%"},
		},
		{
			name: "config structured and request filters combined",
			configFilter: &config.ConfigFilter{
				Structured: &config.Filter{
					Conditions: []config.FilterCondition{
						{Column: "product", Operator: "=", Value: "pgAdmin"},
					},
				},
			},
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "version", Operator: ">=", Value: "v8.0"},
				},
			},
			expectedSQL:  " WHERE (\"product\" = $1) AND (\"version\" >= $2)",
			expectedArgs: []interface{}{"pgAdmin", "v8.0"},
		},
		{
			name: "complex multi-condition filters",
			configFilter: &config.ConfigFilter{
				Structured: &config.Filter{
					Conditions: []config.FilterCondition{
						{Column: "status", Operator: "=", Value: "published"},
						{Column: "category", Operator: "=", Value: "docs"},
					},
					Logic: "AND",
				},
			},
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "product", Operator: "=", Value: "pgAdmin"},
					{Column: "version", Operator: ">=", Value: "v8.0"},
				},
				Logic: "AND",
			},
			expectedSQL:  " WHERE (\"status\" = $1 AND \"category\" = $2) AND (\"product\" = $3 AND \"version\" >= $4)",
			expectedArgs: []interface{}{"published", "docs", "pgAdmin", "v8.0"},
		},
		// Raw SQL config filter tests
		{
			name: "raw SQL config filter",
			configFilter: &config.ConfigFilter{
				RawSQL: "source_id IN (SELECT id FROM documents WHERE product='pgEdge')",
			},
			expectedSQL:  " WHERE (source_id IN (SELECT id FROM documents WHERE product='pgEdge'))",
			expectedArgs: nil,
		},
		{
			name: "raw SQL config filter with request filter",
			configFilter: &config.ConfigFilter{
				RawSQL: "category = 'docs'",
			},
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "version", Operator: ">=", Value: "v8.0"},
				},
			},
			expectedSQL:  " WHERE (category = 'docs') AND (\"version\" >= $1)",
			expectedArgs: []interface{}{"v8.0"},
		},
		{
			name: "raw SQL with subquery",
			configFilter: &config.ConfigFilter{
				RawSQL: "id IN (SELECT doc_id FROM access_control WHERE user_id = 123)",
			},
			expectedSQL:  " WHERE (id IN (SELECT doc_id FROM access_control WHERE user_id = 123))",
			expectedArgs: nil,
		},
		{
			name: "unsupported operator",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "product", Operator: "EXEC", Value: "evil"},
				},
			},
			expectError: true,
		},
		{
			name: "invalid logic operator",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "product", Operator: "=", Value: "test"},
				},
				Logic: "XOR",
			},
			expectError: true,
		},
		{
			name: "empty IN array",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "status", Operator: "IN", Value: []interface{}{}},
				},
			},
			expectError: true,
		},
		{
			name: "nil value for non-NULL operator",
			requestFilter: &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "product", Operator: "=", Value: nil},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := buildFilterClause(tt.configFilter, tt.requestFilter, 1)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if sql != tt.expectedSQL {
				t.Errorf("SQL mismatch:\nexpected: %q\ngot:      %q", tt.expectedSQL, sql)
			}

			if len(args) != len(tt.expectedArgs) {
				t.Errorf("args length mismatch: expected %d, got %d", len(tt.expectedArgs), len(args))
				return
			}

			for i, expected := range tt.expectedArgs {
				if args[i] != expected {
					t.Errorf("arg[%d] mismatch: expected %v, got %v", i, expected, args[i])
				}
			}
		})
	}
}

func TestSQLInjectionPrevention(t *testing.T) {
	injectionAttempts := []struct {
		name  string
		value interface{}
	}{
		{"drop table", "'; DROP TABLE documents; --"},
		{"or condition", "1=1 OR 1=1"},
		{"delete statement", "1'; DELETE FROM users WHERE '1'='1"},
		{"admin bypass", "admin'--"},
		{"comment injection", "' OR '1'='1' /*"},
		{"union select", "' UNION SELECT * FROM users --"},
		{"stacked queries", "'; UPDATE users SET admin=true; --"},
	}

	for _, attempt := range injectionAttempts {
		t.Run(attempt.name, func(t *testing.T) {
			filter := &config.Filter{
				Conditions: []config.FilterCondition{
					{Column: "product", Operator: "=", Value: attempt.value},
				},
			}

			sql, args, err := buildFilterClause(nil, filter, 1)
			if err != nil {
				t.Errorf("unexpected error for injection attempt: %v", err)
				return
			}

			// Verify the injection attempt is treated as a parameter value
			if !strings.Contains(sql, "$1") {
				t.Errorf("SQL does not contain parameter placeholder: %s", sql)
			}

			if len(args) != 1 {
				t.Errorf("expected 1 arg, got %d", len(args))
				return
			}

			if args[0] != attempt.value {
				t.Errorf("arg mismatch: expected %v, got %v", attempt.value, args[0])
			}

			// Verify the malicious content is NOT in the SQL string itself
			maliciousContent := attempt.value.(string)
			if strings.Contains(sql, maliciousContent) {
				t.Errorf("SECURITY VIOLATION: malicious content found in SQL: %s", sql)
			}
		})
	}
}

func TestValidateOperator(t *testing.T) {
	tests := []struct {
		operator    string
		expectError bool
	}{
		{"=", false},
		{"!=", false},
		{"<", false},
		{">", false},
		{"<=", false},
		{">=", false},
		{"LIKE", false},
		{"ILIKE", false},
		{"IN", false},
		{"NOT IN", false},
		{"IS NULL", false},
		{"IS NOT NULL", false},
		// Case insensitive
		{"like", false},
		{"ilike", false},
		// Invalid operators
		{"EXEC", true},
		{"DROP", true},
		{"DELETE", true},
		{"UPDATE", true},
		{"INSERT", true},
		{"--", true},
		{";", true},
	}

	for _, tt := range tests {
		t.Run(tt.operator, func(t *testing.T) {
			err := ValidateOperator(tt.operator)
			if tt.expectError && err == nil {
				t.Errorf("expected error for operator %q, got none", tt.operator)
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error for operator %q: %v", tt.operator, err)
			}
		})
	}
}

func TestValidateValue(t *testing.T) {
	tests := []struct {
		name        string
		operator    string
		value       interface{}
		expectError bool
	}{
		{"equals with string", "=", "test", false},
		{"equals with number", "=", 123, false},
		{"equals with nil", "=", nil, true},
		{"IS NULL with no value", "IS NULL", nil, false},
		{"IS NOT NULL with no value", "IS NOT NULL", nil, false},
		{"IN with array", "IN", []interface{}{"a", "b"}, false},
		{"IN with empty array", "IN", []interface{}{}, true},
		{"IN with non-array", "IN", "test", true},
		{"NOT IN with array", "NOT IN", []interface{}{"a", "b"}, false},
		{"LIKE with string", "LIKE", "%test%", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateValue(tt.operator, tt.value)
			if tt.expectError && err == nil {
				t.Errorf("expected error, got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestBuildFilterClauseWithOffset(t *testing.T) {
	// Test that filters correctly start at the specified parameter index
	// This is important for VectorSearch which uses $1 for vector and $2 for limit
	filter := &config.Filter{
		Conditions: []config.FilterCondition{
			{Column: "product", Operator: "=", Value: "pgAdmin"},
			{Column: "version", Operator: "=", Value: "9.10"},
		},
		Logic: "AND",
	}

	// Start at index 3 (simulating VectorSearch where $1=vector, $2=limit)
	sql, args, err := buildFilterClause(nil, filter, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedSQL := " WHERE (\"product\" = $3 AND \"version\" = $4)"
	if sql != expectedSQL {
		t.Errorf("SQL mismatch:\nexpected: %q\ngot:      %q", expectedSQL, sql)
	}

	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d", len(args))
	}
}

func TestBuildCondition(t *testing.T) {
	tests := []struct {
		name         string
		condition    config.FilterCondition
		expectedSQL  string
		expectedArgs []interface{}
		expectError  bool
	}{
		{
			name:         "simple equality",
			condition:    config.FilterCondition{Column: "status", Operator: "=", Value: "published"},
			expectedSQL:  `"status" = $1`,
			expectedArgs: []interface{}{"published"},
		},
		{
			name:         "greater than",
			condition:    config.FilterCondition{Column: "age", Operator: ">", Value: 18},
			expectedSQL:  `"age" > $1`,
			expectedArgs: []interface{}{18},
		},
		{
			name:         "column with dot in name",
			condition:    config.FilterCondition{Column: "public.users", Operator: "=", Value: "test"},
			expectedSQL:  `"public.users" = $1`,
			expectedArgs: []interface{}{"test"},
		},
		{
			name:         "IN with multiple values",
			condition:    config.FilterCondition{Column: "status", Operator: "IN", Value: []interface{}{"a", "b", "c"}},
			expectedSQL:  `"status" IN ($1, $2, $3)`,
			expectedArgs: []interface{}{"a", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paramIndex := 1
			sql, args, err := buildCondition(tt.condition, &paramIndex)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if sql != tt.expectedSQL {
				t.Errorf("SQL mismatch:\nexpected: %q\ngot:      %q", tt.expectedSQL, sql)
			}

			if len(args) != len(tt.expectedArgs) {
				t.Errorf("args length mismatch: expected %d, got %d", len(tt.expectedArgs), len(args))
				return
			}

			for i, expected := range tt.expectedArgs {
				if args[i] != expected {
					t.Errorf("arg[%d] mismatch: expected %v, got %v", i, expected, args[i])
				}
			}
		})
	}
}
