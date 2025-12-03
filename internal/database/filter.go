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
	"fmt"
	"strings"

	"github.com/pgEdge/pgedge-rag-server/internal/config"

	"github.com/jackc/pgx/v5"
)

// supportedOperators defines the allowed SQL operators for security.
var supportedOperators = map[string]bool{
	"=":           true,
	"!=":          true,
	"<":           true,
	">":           true,
	"<=":          true,
	">=":          true,
	"LIKE":        true,
	"ILIKE":       true,
	"IN":          true,
	"NOT IN":      true,
	"IS NULL":     true,
	"IS NOT NULL": true,
}

// buildFilterClause constructs a parameterized WHERE clause from config and request filters.
// Returns the WHERE clause string, parameter values, and any error.
// The WHERE clause uses PostgreSQL parameter placeholders starting from startParamIndex.
//
// Config filters can be raw SQL strings (admin-controlled, trusted) or structured filters.
// Request filters must be structured filters (user input, parameterized for security).
func buildFilterClause(configFilter *config.ConfigFilter, requestFilter *config.Filter, startParamIndex int) (string, []interface{}, error) {
	var conditions []string
	var args []interface{}
	paramIndex := startParamIndex

	// Process config-level filter (can be raw SQL or structured)
	if configFilter != nil {
		if configFilter.RawSQL != "" {
			// Raw SQL from config file - admin controlled, trusted
			conditions = append(conditions, "("+configFilter.RawSQL+")")
		} else if configFilter.Structured != nil {
			clause, clauseArgs, err := buildFilterFromStruct(configFilter.Structured, &paramIndex)
			if err != nil {
				return "", nil, fmt.Errorf("config filter error: %w", err)
			}
			if clause != "" {
				conditions = append(conditions, "("+clause+")")
				args = append(args, clauseArgs...)
			}
		}
	}

	// Process request-level filter (must be structured, parameterized for security)
	if requestFilter != nil {
		clause, clauseArgs, err := buildFilterFromStruct(requestFilter, &paramIndex)
		if err != nil {
			return "", nil, fmt.Errorf("request filter error: %w", err)
		}
		if clause != "" {
			conditions = append(conditions, "("+clause+")")
			args = append(args, clauseArgs...)
		}
	}

	if len(conditions) == 0 {
		return "", nil, nil
	}

	return " WHERE " + strings.Join(conditions, " AND "), args, nil
}

// buildFilterFromStruct converts a Filter struct to SQL WHERE conditions.
// Returns the SQL string (without WHERE keyword), parameter values, and any error.
func buildFilterFromStruct(filter *config.Filter, paramIndex *int) (string, []interface{}, error) {
	if filter == nil || len(filter.Conditions) == 0 {
		return "", nil, nil
	}

	logic := "AND"
	if filter.Logic != "" {
		logic = strings.ToUpper(filter.Logic)
		if logic != "AND" && logic != "OR" {
			return "", nil, fmt.Errorf("invalid logic operator: %s (must be AND or OR)", logic)
		}
	}

	conditions := make([]string, 0, len(filter.Conditions))
	var args []interface{}

	for _, cond := range filter.Conditions {
		clause, clauseArgs, err := buildCondition(cond, paramIndex)
		if err != nil {
			return "", nil, err
		}
		conditions = append(conditions, clause)
		args = append(args, clauseArgs...)
	}

	return strings.Join(conditions, " "+logic+" "), args, nil
}

// buildCondition constructs a single filter condition with parameterized values.
// Returns the SQL clause string, parameter values, and any error.
func buildCondition(cond config.FilterCondition, paramIndex *int) (string, []interface{}, error) {
	// Validate operator
	if err := ValidateOperator(cond.Operator); err != nil {
		return "", nil, err
	}

	// Validate value for operator
	if err := ValidateValue(cond.Operator, cond.Value); err != nil {
		return "", nil, err
	}

	// Sanitize column name using pgx.Identifier
	columnName := pgx.Identifier{cond.Column}.Sanitize()
	op := strings.ToUpper(cond.Operator)

	// Handle NULL operators (no value needed)
	if op == "IS NULL" || op == "IS NOT NULL" {
		return fmt.Sprintf("%s %s", columnName, op), nil, nil
	}

	// Handle IN operator (expects array)
	if op == "IN" || op == "NOT IN" {
		values, ok := cond.Value.([]interface{})
		if !ok {
			return "", nil, fmt.Errorf("IN operator requires array value")
		}
		placeholders := make([]string, len(values))
		args := make([]interface{}, len(values))
		for i, v := range values {
			placeholders[i] = fmt.Sprintf("$%d", *paramIndex)
			args[i] = v
			*paramIndex++
		}
		clause := fmt.Sprintf("%s %s (%s)", columnName, op, strings.Join(placeholders, ", "))
		return clause, args, nil
	}

	// Standard operators with single value
	placeholder := fmt.Sprintf("$%d", *paramIndex)
	*paramIndex++
	clause := fmt.Sprintf("%s %s %s", columnName, cond.Operator, placeholder)
	return clause, []interface{}{cond.Value}, nil
}

// ValidateOperator checks if an operator is in the allowed list.
func ValidateOperator(operator string) error {
	op := strings.ToUpper(operator)
	if !supportedOperators[op] {
		return fmt.Errorf("unsupported operator: %s (allowed: =, !=, <, >, <=, >=, LIKE, ILIKE, IN, NOT IN, IS NULL, IS NOT NULL)", operator)
	}
	return nil
}

// ValidateValue validates that the value is appropriate for the given operator.
func ValidateValue(operator string, value interface{}) error {
	op := strings.ToUpper(operator)

	// NULL operators don't need values
	if op == "IS NULL" || op == "IS NOT NULL" {
		return nil
	}

	// IN operators need arrays
	if op == "IN" || op == "NOT IN" {
		switch v := value.(type) {
		case []interface{}:
			if len(v) == 0 {
				return fmt.Errorf("IN operator requires non-empty array")
			}
		default:
			return fmt.Errorf("IN operator requires array value, got: %T", value)
		}
		return nil
	}

	// Other operators need non-nil single value
	if value == nil {
		return fmt.Errorf("operator %s requires non-nil value", operator)
	}

	return nil
}
