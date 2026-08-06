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
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgEdge/pgedge-rag-server/internal/config"
)

// ErrEnforcementUnverified reports that the database cannot be shown to
// enforce the identity this server presents. Every wrapped case has the
// same shape: retrieval will run, return rows, and raise no error, whilst
// row-level security either does not apply or is evaluating an identity
// other than the caller's.
var ErrEnforcementUnverified = errors.New("identity enforcement could not be verified")

// enforcementSQL reads what the database will do with a configured
// table, for the role this pool is connected as.
//
// relrowsecurity / relforcerowsecurity, the ownership test and
// rolsuper/rolbypassrls together decide whether policies run at all —
// a superuser bypasses row-level security whether or not it carries
// BYPASSRLS, so both attributes are read; pg_get_expr over pg_policy
// gives the policy text, which is what the pinning check reads. The
// table name is bound as a parameter and cast to regclass so a
// configured name cannot be concatenated into this statement.
//
// Only policies that govern reads are collected, and only their USING
// clause:
//
//   - polcmd is restricted to 'r' (FOR SELECT) and '*' (FOR ALL). A
//     policy that only governs writes says nothing about what a caller
//     may see, so counting it would let a table whose SELECT policy is
//     USING (true) pass the check on the strength of a claims-aware
//     FOR INSERT policy — wide open to every caller, and reported as
//     verified.
//   - polwithcheck is excluded for the same reason. WITH CHECK
//     constrains rows being written, never rows being returned, and
//     this server only ever reads.
const enforcementSQL = `
SELECT
	c.relkind::text,
	c.relrowsecurity,
	c.relforcerowsecurity,
	pg_catalog.pg_has_role(current_user, c.relowner, 'USAGE') AS owns_table,
	(SELECT r.rolsuper OR r.rolbypassrls FROM pg_catalog.pg_roles r
	  WHERE r.rolname = current_user) AS bypasses_rls,
	COALESCE(
		(SELECT array_agg(
			COALESCE(pg_catalog.pg_get_expr(p.polqual, p.polrelid), ''))
		   FROM pg_catalog.pg_policy p
		  WHERE p.polrelid = c.oid
		    AND p.polcmd IN ('r', '*')),
		ARRAY[]::text[]) AS policy_exprs
FROM pg_catalog.pg_class c
WHERE c.oid = $1::regclass`

// isUndefinedObject reports whether err is PostgreSQL complaining that a
// name does not resolve — an unknown table, schema or other object.
func isUndefinedObject(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case "42P01", // undefined_table
		"42703", // undefined_column
		"3F000": // invalid_schema_name
		return true
	}
	return false
}

// VerifyEnforcement checks, at startup, that the identity this server
// will present is the identity the database will act on for each of the
// given tables.
//
// It exists because the failure it looks for is silent. If a table has
// no row-level security, or the connecting role bypasses it, or its
// policies resolve identity from somewhere other than the claims this
// server sets, then queries succeed and return rows and the only thing
// wrong is that the wrong caller can see them. There is no error to
// notice in production, so this refuses at startup instead.
//
// The pinning case in particular is worth naming. A deployment may
// resolve a fixed identity from a table keyed on session_user —
// deliberately, so that a service which executes caller-supplied SQL
// cannot forge one by setting request.jwt.claims itself. Against such a
// database this server's claims are accepted and then ignored. The
// detection here is textual: if no policy on a table mentions the
// configured claims parameter, the policies are keyed on something
// else, and that something else is not the caller.
//
// Two honest limits on that:
//
//   - A policy that calls a function which reads the claims parameter
//     internally will not mention it in its own text, and will be
//     reported as unverified even though it is correct. That is a false
//     positive, and the reason enforcement_check has a warn and an off
//     setting.
//   - A policy that mentions the parameter is not thereby proven to use
//     it correctly. This confirms the claims are consulted, not that the
//     resulting rule is the one the operator intended.
//
// Erring towards the false positive is deliberate: a spurious refusal
// at startup is visible and costs an operator a configuration change,
// whilst a missed detection costs a tenant their data.
func (p *Pool) VerifyEnforcement(
	ctx context.Context,
	tables []config.TableSource,
) []error {
	if !p.identity.Enabled || p.identity.EnforcementCheck == config.EnforcementCheckOff {
		return nil
	}

	var problems []error

	problems = append(problems, p.verifyAllowedRoles(ctx, tables)...)

	for _, table := range tables {
		if err := p.verifyTable(ctx, table.Table); err != nil {
			problems = append(problems, err)
		}
	}
	return problems
}

// bypassingRolesSQL finds roles that row-level security does not apply
// to at all, among a given set of names.
const bypassingRolesSQL = `
SELECT r.rolname
  FROM pg_catalog.pg_roles r
 WHERE r.rolname = ANY($1::text[])
   AND (r.rolsuper OR r.rolbypassrls)
 ORDER BY r.rolname`

// owningRolesSQL finds roles that own a given relation, among a set of
// names, where the relation does not force row-level security on its
// owner.
const owningRolesSQL = `
SELECT r.rolname
  FROM pg_catalog.pg_roles r, pg_catalog.pg_class c
 WHERE r.rolname = ANY($1::text[])
   AND c.oid = $2::regclass
   AND NOT c.relforcerowsecurity
   AND pg_catalog.pg_has_role(r.rolname, c.relowner, 'USAGE')
 ORDER BY r.rolname`

// verifyAllowedRoles checks the roles a request may be switched into.
//
// The per-table check reads the attributes of the role the pool logs in
// as, which is the identity every query starts from. But when
// identity.allowed_roles is non-empty a request can leave that role
// behind: the claims name a role, this server assumes it, and the query
// runs as that role instead. If one of those roles is a superuser or
// carries BYPASSRLS, every policy on every table is skipped for any
// caller who claims it — and the per-table check, which never looks at
// that role, reports the table as verified.
//
// That is precisely the shape of failure this preflight exists to
// catch, so it is checked here as well. Two ways a claimable role can
// escape policy are covered, because they are separate mechanisms:
//
//   - the role is a superuser or carries BYPASSRLS, which skips every
//     policy on every table; and
//   - the role owns one of the configured tables, since an owner is
//     exempt from its own policies unless the table has FORCE ROW LEVEL
//     SECURITY. This is the same exemption the per-table check applies
//     to the connecting role, and it is no less dangerous for a role
//     the caller can ask to become.
func (p *Pool) verifyAllowedRoles(
	ctx context.Context,
	tables []config.TableSource,
) []error {
	if len(p.identity.AllowedRoles) == 0 {
		return nil
	}

	var problems []error

	bypassing, err := p.rolesMatching(ctx, bypassingRolesSQL, p.identity.AllowedRoles)
	if err != nil {
		problems = append(problems, err)
	} else if len(bypassing) > 0 {
		problems = append(problems, fmt.Errorf(
			"%w: identity.allowed_roles names %s, which %s row-level security "+
				"(superuser or BYPASSRLS); a caller whose claims name such a role "+
				"would see every row of every table, so it must not be reachable "+
				"through a claim",
			ErrEnforcementUnverified, strings.Join(quoteAll(bypassing), ", "),
			plural(len(bypassing), "bypasses", "bypass")))
	}

	for _, table := range tables {
		owners, err := p.rolesMatching(ctx, owningRolesSQL,
			p.identity.AllowedRoles, table.Table)
		if err != nil {
			// A relation that does not resolve is reported once, by the
			// per-table check; repeating it here would be noise.
			if !errors.Is(err, errRelationUnresolved) {
				problems = append(problems, err)
			}
			continue
		}
		if len(owners) == 0 {
			continue
		}

		problems = append(problems, fmt.Errorf(
			"%w: identity.allowed_roles names %s, which %s %q, and an owner is "+
				"exempt from its own policies unless the table has FORCE ROW LEVEL "+
				"SECURITY; a caller whose claims name such a role would see every "+
				"row of it. Run ALTER TABLE %s FORCE ROW LEVEL SECURITY, or remove "+
				"the role from identity.allowed_roles",
			ErrEnforcementUnverified, strings.Join(quoteAll(owners), ", "),
			plural(len(owners), "owns", "own"), table.Table, table.Table))
	}

	return problems
}

// errRelationUnresolved marks a lookup that failed because the
// configured relation does not resolve, so callers can leave that
// finding to the per-table check rather than reporting it twice.
var errRelationUnresolved = errors.New("relation does not resolve")

// rolesMatching runs a role-selecting query and collects the names it
// returns.
func (p *Pool) rolesMatching(
	ctx context.Context,
	sql string,
	args ...any,
) ([]string, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		if isUndefinedObject(err) {
			return nil, errRelationUnresolved
		}
		return nil, fmt.Errorf("%w: could not inspect identity.allowed_roles: %w",
			ErrEnforcementUnverified, err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("%w: could not inspect identity.allowed_roles: %w",
				ErrEnforcementUnverified, err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		if isUndefinedObject(err) {
			return nil, errRelationUnresolved
		}
		return nil, fmt.Errorf("%w: could not inspect identity.allowed_roles: %w",
			ErrEnforcementUnverified, err)
	}

	return names, nil
}

// quoteAll renders names for an error message.
func quoteAll(names []string) []string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = fmt.Sprintf("%q", name)
	}
	return quoted
}

// plural picks a verb form for a count.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// verifyTable performs the checks described on VerifyEnforcement for a
// single table, in order of severity: the ones that disable policies
// entirely come before the one about which identity the policies read.
func (p *Pool) verifyTable(ctx context.Context, table string) error {
	var (
		relkind      string
		rowSecurity  bool
		forceRLS     bool
		ownsTable    bool
		bypassesRLS  *bool
		policyExprs  []string
		claimSetting = p.identity.ClaimsSetting
	)

	err := p.pool.QueryRow(ctx, enforcementSQL, table).Scan(
		&relkind, &rowSecurity, &forceRLS, &ownsTable, &bypassesRLS, &policyExprs)
	// A configured name that does not resolve fails in the ::regclass
	// cast rather than returning no rows, so the "missing relation" case
	// arrives as a PostgreSQL error and not as pgx.ErrNoRows. Both are
	// handled: ErrNoRows would be the outcome if the cast ever resolved
	// to an oid with no pg_class row.
	if errors.Is(err, pgx.ErrNoRows) || isUndefinedObject(err) {
		return fmt.Errorf("%w: relation %q was not found, or the connecting role "+
			"cannot see it", ErrEnforcementUnverified, table)
	}
	if err != nil {
		return fmt.Errorf("%w: could not inspect %q: %w",
			ErrEnforcementUnverified, table, err)
	}

	// A view has no policies of its own; what it can see depends on
	// whether it was created WITH (security_invoker), which is not
	// something this check can usefully second-guess. Report it rather
	// than pass it silently.
	if relkind == "v" || relkind == "m" {
		return fmt.Errorf(
			"%w: %q is a view, whose visibility depends on its own definition "+
				"rather than on policies this check can read; confirm it was created "+
				"WITH (security_invoker = true) and that its underlying tables "+
				"enforce row-level security, then set identity.enforcement_check to "+
				"warn or off",
			ErrEnforcementUnverified, table)
	}

	if bypassesRLS != nil && *bypassesRLS {
		return fmt.Errorf(
			"%w: the role this pipeline connects as is a superuser or has "+
				"BYPASSRLS, so no policy on %q will be applied to any caller; "+
				"connect as an ordinary role instead",
			ErrEnforcementUnverified, table)
	}

	if !rowSecurity {
		return fmt.Errorf(
			"%w: row-level security is not enabled on %q, so every caller would see "+
				"every row regardless of the identity presented; run ALTER TABLE %s "+
				"ENABLE ROW LEVEL SECURITY and add a policy keyed on %s",
			ErrEnforcementUnverified, table, table, claimSetting)
	}

	// A table's owner is exempt from its own policies unless the table
	// has FORCE ROW LEVEL SECURITY. This is the one that most often
	// catches a deployment out: the policies exist, they read the right
	// parameter, they test correctly in psql as another role, and they
	// do nothing at all for this server because the service role happens
	// to own the table.
	if ownsTable && !forceRLS {
		return fmt.Errorf(
			"%w: the role this pipeline connects as owns %q, and owners are exempt "+
				"from their own policies unless the table has FORCE ROW LEVEL "+
				"SECURITY; run ALTER TABLE %s FORCE ROW LEVEL SECURITY, or connect "+
				"as a role that does not own the table",
			ErrEnforcementUnverified, table, table)
	}

	if len(policyExprs) == 0 {
		return fmt.Errorf(
			"%w: row-level security is enabled on %q but no policy governs SELECT, "+
				"so every caller would see no rows at all; a policy declared FOR "+
				"INSERT, UPDATE or DELETE does not decide what a query may read",
			ErrEnforcementUnverified, table)
	}

	for _, expr := range policyExprs {
		if strings.Contains(expr, claimSetting) {
			return nil
		}
	}

	return fmt.Errorf(
		"%w: no SELECT policy on %q refers to %s in its USING clause, so the "+
			"identity this "+
			"server sets is being discarded and the policies are keyed on something "+
			"else — commonly a table keyed on session_user that pins a fixed identity "+
			"for the service login role. The database half of this change has to land "+
			"alongside the server half. If a policy reads %s indirectly through a "+
			"function, this check cannot see it: set identity.enforcement_check to "+
			"warn or off once you have confirmed that by hand",
		ErrEnforcementUnverified, table, claimSetting, claimSetting)
}
