//-------------------------------------------------------------------------
//
// pgEdge RAG Server
//
// Portions copyright (c) 2025, pgEdge, Inc.
// This software is released under The PostgreSQL License
//
//-------------------------------------------------------------------------

package database

import (
	"testing"
)

func TestBuildFilterClause(t *testing.T) {
	tests := []struct {
		name          string
		configFilter  string
		requestFilter string
		expected      string
	}{
		{
			name:          "no filters",
			configFilter:  "",
			requestFilter: "",
			expected:      "",
		},
		{
			name:          "config filter only",
			configFilter:  "product = 'pgAdmin'",
			requestFilter: "",
			expected:      " WHERE (product = 'pgAdmin')",
		},
		{
			name:          "request filter only",
			configFilter:  "",
			requestFilter: "version = 'v9.0'",
			expected:      " WHERE (version = 'v9.0')",
		},
		{
			name:          "both filters",
			configFilter:  "product = 'pgAdmin'",
			requestFilter: "version = 'v9.0'",
			expected:      " WHERE (product = 'pgAdmin') AND (version = 'v9.0')",
		},
		{
			name:          "complex config filter",
			configFilter:  "status = 'published' AND category = 'docs'",
			requestFilter: "",
			expected:      " WHERE (status = 'published' AND category = 'docs')",
		},
		{
			name:          "complex both filters",
			configFilter:  "status = 'published'",
			requestFilter: "product = 'pgAdmin' AND version >= 'v8.0'",
			expected:      " WHERE (status = 'published') AND (product = 'pgAdmin' AND version >= 'v8.0')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildFilterClause(tt.configFilter, tt.requestFilter)
			if result != tt.expected {
				t.Errorf("buildFilterClause(%q, %q) = %q, want %q",
					tt.configFilter, tt.requestFilter, result, tt.expected)
			}
		})
	}
}
