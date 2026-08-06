# Per-Request Identity

By default, every query the RAG Server runs against a pipeline's tables
executes as the database role in that pipeline's `database:` block. The
server has no notion of who asked. A corpus shared between users or
tenants therefore cannot be scoped per caller: the operator must either
grant the service role everything the most privileged caller may see, or
restrict it to what the least privileged caller may see.

Per-request identity changes that. The claims that arrive with a request
are applied to the database session for the lifetime of that one query,
and PostgreSQL's own row-level security decides what the query may see.
The model is PostgREST's, and policies written for PostgREST work
unchanged.

The server makes no authorisation decisions of its own. It presents an
identity; the database enforces the policies written against it.

!!! warning "Two halves that must land together"

    Enabling this in the server does nothing on its own. The database
    must have row-level security enabled on every configured table, with
    policies keyed on the claims the server sets, and the pipeline must
    connect as a role those policies actually apply to. The server
    checks all of that at startup and refuses to start if it cannot
    confirm it — see [Startup enforcement checks](#startup-enforcement-checks).

## Who may assert an identity

This is the load-bearing assumption of the whole feature, so it is
stated first.

The server reads claims from ordinary HTTP request headers. **It does
not verify them.** It does not check a signature, fetch a JWKS, or
validate an issuer, audience or expiry. It treats the configured headers
as already verified and applies them to the database session.

That means:

> Only a trusted component may set the claims and subject headers on a
> request that reaches this server. That component must strip any
> inbound copy of those headers from client input, and re-set them from
> a credential it has itself verified.

In practice that component is an ingress proxy, API gateway, or service
mesh sidecar. If a caller can reach the server's listening port
directly, that caller can assert any identity it likes — and row-level
security will faithfully enforce the identity it was handed.

Three things follow, and all three are part of a correct deployment:

- Bind the server to a private interface, or otherwise ensure only the
  proxy can reach its port.
- Configure the proxy to strip the identity headers from client input
  before setting its own. Most proxies do not do this by default.
- Set `identity.trusted_proxies` so the server also refuses requests
  whose immediate peer address is outside the expected range. This
  checks the TCP peer, which is the one part of a request a client
  cannot choose. It is a second line behind network policy, not a
  replacement for it.

### Why the server does not verify JWTs itself

It would be possible for the server to verify a signed token directly —
parse a JWS, fetch and cache a JWKS, check `iss`, `aud` and `exp` — and
so depend less on the proxy. That was considered and deliberately not
done here:

- It duplicates a component nearly every deployment already runs. An
  ingress that is not already authenticating requests is not a
  deployment where per-tenant retrieval is safe anyway.
- It puts key fetching, caching and rotation inside the retrieval path,
  which is a new failure mode on a hot path: a JWKS endpoint that is
  slow or briefly unreachable would start failing queries.
- It does not remove the trust boundary, it moves it. Whoever terminates
  TLS in front of the server can still inject headers, so the network
  posture above is required either way.

The extension point is left open: identity extraction lives behind
`identity.Extractor` in `internal/identity`, and a token-verifying
source can be added there without touching the database layer. If your
deployment has no trustworthy component in front of the server, that
work is a prerequisite — the configuration described here is not a
substitute for it.

## Enabling it

```yaml
identity:
    enabled: true
    claims_header: X-Forwarded-Claims
    subject_header: X-Forwarded-User
    claims_setting: request.jwt.claims
    subject_claim: sub
    role_claim: role
    allowed_roles:
        - rag_tenant
    trusted_proxies:
        - 10.0.0.0/8
    enforcement_check: error
    allow_shared_vector_index: false
```

Identity is configured once, at the top level, rather than per pipeline:
a control that varied between pipelines by accident would be worse than
no control.

### Properties

| Property | Default | Description |
|----------|---------|-------------|
| `enabled` | `false` | Turn per-request identity on. When off, behaviour is exactly as before: every query runs as the pipeline's own role. |
| `claims_header` | `X-Forwarded-Claims` | Request header carrying the caller's verified claims as a JSON object. |
| `subject_header` | `X-Forwarded-User` | Fallback header carrying a bare subject string, wrapped as `{"<subject_claim>": "<value>"}`. Set to `-` to require a full claim set. |
| `claims_setting` | `request.jwt.claims` | PostgreSQL run-time parameter the claim set is written to, with `SET LOCAL` semantics. |
| `subject_claim` | `sub` | Claim used to label requests in the log, and the key used to wrap `subject_header`. |
| `role_claim` | `role` | Claim that may request a PostgreSQL role for the query. Set to `-` to ignore role claims entirely. |
| `allowed_roles` | *(empty)* | Roles a request may be switched to. Empty disables role switching: a request claiming a role is refused, not served with the role ignored. |
| `trusted_proxies` | *(empty)* | CIDR blocks. When set, a request whose peer address is outside every block is refused before its headers are read. |
| `enforcement_check` | `error` | Startup preflight behaviour: `error`, `warn` or `off`. |
| `allow_shared_vector_index` | `false` | Permit an approximate vector index shared between identities. See [The shared vector index](#the-shared-vector-index). |

### What the caller sends

Either a full claim set:

```
X-Forwarded-Claims: {"sub":"alice","role":"rag_tenant","tenant":"acme"}
```

or, for a proxy that can assert who the caller is but cannot emit JSON,
a bare subject:

```
X-Forwarded-User: alice
```

which becomes `{"sub":"alice"}`. The claims header wins when both are
present, so a proxy that can send a full claim set is never silently
downgraded.

The claim set is passed to PostgreSQL byte for byte. It is never
re-encoded, so a policy that digs into a nested claim sees exactly what
the proxy asserted.

### What the database sees

For the lifetime of each query, inside a read-only transaction:

```sql
SELECT set_config('request.jwt.claims', '{"sub":"alice"}', true),
       set_config('role', 'rag_tenant', true),
       ...
```

Policies then read it in the usual way:

```sql
ALTER TABLE chunks ENABLE ROW LEVEL SECURITY;

CREATE POLICY own_rows ON chunks FOR SELECT
    USING (owner = current_setting('request.jwt.claims', true)::json->>'sub');
```

Every value is set with `SET LOCAL` semantics and the transaction is
always rolled back, so nothing survives onto the next request that
acquires the same pooled connection.

## What happens without an identity

A request that carries no identity is **refused**. There is no fallback
to the pipeline's own database role.

This is deliberate. Falling back would reintroduce the shared-role
bypass in exactly the case nobody is watching — the request that arrived
without the header because the proxy was misconfigured or bypassed. A
refusal is noisy; a silent downgrade to full-corpus access is not.

The refusal is distinguishable from other failures:

| Code | Status | Meaning |
|------|--------|---------|
| `IDENTITY_REQUIRED` | 401 | No identity was presented. |
| `IDENTITY_MALFORMED` | 400 | The claims header was present but was not a JSON object. |
| `IDENTITY_UNTRUSTED_PEER` | 403 | The request's peer address is not permitted to assert an identity. |
| `IDENTITY_ROLE_NOT_ALLOWED` | 403 | The claims named a database role that is not in `allowed_roles`. |

Only the query endpoint requires an identity. `/v1/live`, `/v1/health`,
`/v1/pipelines`, `/v1/stats` and `/v1/openapi.json` read no corpus, so
probes and discovery keep working unchanged.

## Startup enforcement checks

When identity is enabled, the server inspects every configured table
before serving anything, and refuses to start if the database will not
act on the identity it presents.

This exists because the failures it looks for are silent. In every case
below, queries succeed and return rows; the only thing wrong is that the
wrong caller can see them.

The checks, in order:

- **The relation resolves.** A configured name the connecting role
  cannot see would otherwise fail per request.
- **The connecting role does not have `BYPASSRLS`.** Such a role ignores
  every policy on every table.
- **Row-level security is enabled** on the table. Without it, every
  caller sees every row whatever identity is presented.
- **The connecting role does not own the table**, unless the table has
  `FORCE ROW LEVEL SECURITY`. An owner is exempt from its own policies.
  This is the one that most often catches a deployment out: the policies
  exist, they read the right parameter, they test correctly in `psql` as
  another role — and they do nothing at all for the server, because the
  service role happens to own the table.
- **At least one policy is defined.**
- **At least one policy refers to the configured claims parameter.**

### Database-side identity pinning

That last check is the interesting one. A deployment may already pin
identity inside the database: a policy that resolves a fixed identity
from a table keyed on `session_user`, deliberately ignoring
`request.jwt.claims` so that a service which executes caller-supplied
SQL cannot forge one.

```sql
-- The pattern this check looks for
CREATE POLICY pinned_rows ON chunks FOR SELECT
    USING (owner = (SELECT tenant FROM service_identity
                     WHERE login_role = session_user));
```

Against such a database, the claims this server sets are accepted
without error and then discarded. Every query works. Every test passes.
Row-level security evaluates as the pinned identity for every caller,
and nothing at request time can tell.

The detection is textual: if no policy on a table mentions the
configured claims parameter, the policies are keyed on something else,
and that something else is not the caller.

Two honest limits:

- A policy that reads the claims parameter indirectly, through a
  function, will not mention it in its own text and will be reported as
  unverified even though it is correct.
- A policy that mentions the parameter is not thereby proven to use it
  correctly. The check confirms the claims are consulted, not that the
  resulting rule is the one you intended.

Erring towards the false positive is deliberate: a spurious refusal at
startup is visible and costs a configuration change, whilst a missed
detection costs a tenant their data. Set `enforcement_check: warn` once
you have confirmed by hand that a flagged table is correct.

## The shared vector index

!!! danger "Read this before serving a multi-tenant corpus"

    Once retrieval is scoped per person, a vector index shared between
    identities becomes an information channel between them — even though
    no forbidden row is ever returned.

pgvector applies row-level security as a filter **on top of** an index
scan that has already chosen its candidates from the whole corpus. The
scan has a bounded candidate budget (`hnsw.ef_search`), so when a
caller's own rows are a small minority of the corpus, that budget is
spent on rows she may not see, and the query returns fewer rows than
asked for — often none.

Measured on PostgreSQL 16.13 with pgvector 0.6.0: 20,000 rows owned by
one identity, 50 by another, HNSW index on `vector_cosine_ops`, a policy
keyed on the claims parameter. The second identity asks for her ten
nearest chunks:

```
 Limit (actual rows=0 loops=1)
   ->  Index Scan using chunks_embedding_idx on chunks (actual rows=0 loops=1)
         Order By: (embedding <=> '[0.5,...]'::vector)
         Filter: (owner = ((current_setting('request.jwt.claims'::text, true))::json ->> 'sub'::text))
         Rows Removed by Filter: 391
```

Zero rows. An exact scan over the same data returns all ten. The same
behaviour is reported on pgvector 0.8.5 (`Rows Removed by Filter: 391`,
and 3,107 with iterative scan enabled), so it is not a quirk of one
version.

The filtering itself is correct — no row belonging to another identity
is ever disclosed. But **how many** rows come back, and **how long** the
query takes, are both functions of how many rows the caller may *not*
see happen to lie near her query vector. A caller who chooses query
vectors can therefore probe the density of another tenant's corpus in
embedding space, and embedding inversion turns a position in that space
back into approximate text.

Filtering harder does not help. The filtering is what produces the
signal; a stricter policy makes the deficit larger.

### What the server does about it

When identity is enabled, `allow_shared_vector_index` defaults to
`false`, and the identity-bearing transaction disables index and bitmap
scans:

```sql
SELECT set_config('enable_indexscan', 'off', true),
       set_config('enable_bitmapscan', 'off', true)
```

The vector arm then performs an exact scan. Every caller gets the rows
she asked for, and the result count carries no information about anyone
else's corpus. The cost is that vector search becomes O(n) in the table
size rather than approximate — which is the price of a shared table
serving multiple identities, and is why the alternative below exists.

### The alternative: per-identity indexes

If exact scanning is too slow for your corpus, the fix is not to filter
harder but to stop sharing the approximate structure. Partition the
table by tenant, or build partial indexes per tenant, so that the index
answering a query only ever contains rows that caller may see. Then set
`allow_shared_vector_index: true` — at that point the index is not
shared, and the setting's name no longer describes what you have built.

Setting `allow_shared_vector_index: true` on a genuinely shared index is
only safe when every identity that can reach a table is permitted to
know the shape of everything in it — a single-tenant corpus split by
document category, say, rather than one split by customer.

## A worked example

Database side:

```sql
-- A service login role that owns nothing and bypasses nothing
CREATE ROLE rag_service LOGIN PASSWORD 'redacted';

CREATE TABLE chunks (
    id       bigserial PRIMARY KEY,
    owner    text NOT NULL,
    content  text NOT NULL,
    embedding vector(1536)
);

ALTER TABLE chunks ENABLE ROW LEVEL SECURITY;

CREATE POLICY own_rows ON chunks FOR SELECT
    USING (owner = current_setting('request.jwt.claims', true)::json->>'sub');

GRANT SELECT ON chunks TO rag_service;
```

Server side:

```yaml
identity:
    enabled: true
    trusted_proxies:
        - 10.0.0.0/8

pipelines:
    - name: docs
      database:
          host: postgres.internal
          database: ragdb
          username: rag_service
      tables:
          - table: chunks
            text_column: content
            vector_column: embedding
            id_column: id
```

Proxy side: verify the caller's token, then set
`X-Forwarded-Claims: {"sub":"<verified subject>"}`, having first
stripped any inbound copy of that header.

## Limitations

- Claims are trusted, not verified. See
  [Who may assert an identity](#who-may-assert-an-identity).
- The enforcement preflight runs at startup, against the tables
  configured at that moment. A policy dropped while the server is
  running is not detected.
- The preflight's pinning detection is textual, with the two limits
  noted above.
- Enabling identity adds a transaction (one extra round trip to begin,
  one to apply the identity and roll back) to each query, and — unless
  you opt into a shared index — makes the vector arm an exact scan.
