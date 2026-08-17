# Changelog

All notable changes to the pgEdge RAG Server will be
documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0-beta1] - 2026-08-17

### Security

- **Breaking:** `include_sources` on a query is now honoured only when
  the pipeline sets the new `allow_include_sources: true` option, which
  defaults to `false`. Previously any client could ask for the raw
  stored content of matching rows and receive it, which made every row
  in a configured table directly retrievable by anyone able to reach
  the query endpoint, independently of anything the LLM decided to
  say. The two gates are deliberately separate, so permitting sources
  does not force them on clients that do not want them, and an
  operator can withdraw permission without a client deploy. Requests
  that ask for sources from a pipeline that does not permit them still
  succeed and return the answer without a `sources` array, so nothing
  breaks beyond the sources themselves disappearing until the option is
  set. Pipelines that legitimately serve a public corpus should add
  `allow_include_sources: true`.

- Retrieved documents are now framed as untrusted data rather than
  concatenated into the system prompt. Content previously went into the
  system prompt behind a fixed `--- Document N ---` header, with no
  escaping and no language distinguishing reference material from
  instructions, which put text an attacker could control in the
  highest-authority position the provider offers. Documents now travel
  in the user turn between `BEGIN`/`END` markers carrying a nonce that
  is random per request, so content cannot forge a closing marker, and
  the system prompt carries a matching block of security rules that is
  appended beneath any custom `system_prompt` and cannot be overridden
  from configuration. The rules forbid acting on instructions found in
  retrieved text, soliciting credentials, asserting that an account is
  locked, and directing users somewhere to log in or pay. Prompt
  injection has no complete fix, so this reduces risk rather than
  removing it, and keeping the corpus trustworthy still matters.

- Context documents now carry their source id, so the rendered block
  attributes each document. The attribution header existed but was
  never populated, leaving the model nothing to reason about when
  deciding what a claim rests on.

- Updated `github.com/jackc/pgx/v5` from 5.9.1 to 5.9.2, which fixes
  CVE-2026-41889 (GO-2026-5004), a SQL injection in the query sanitiser
  arising from placeholder confusion with dollar-quoted string
  literals. `govulncheck` reports the affected symbol as reachable from
  `database.Pool.FetchDocumentsByIDs`, although that reachability is
  static: the sanitiser is only invoked on pgx's simple protocol path,
  and this server leaves the query execution mode at pgx's default of
  `cache_statement`, which sends arguments as bind parameters instead.
  The upgrade is worth taking regardless, since the analysis rests on a
  default we do not pin and a later change to the simple protocol would
  silently reintroduce the exposure.

- Updated `golang.org/x/text` from 0.35.0 to 0.39.0, fixing GO-2026-5970,
  an infinite loop on invalid input. This one is reachable from
  `database.NewPool` by way of `pgxpool.NewWithConfig`, so it sits on
  the startup path of every pipeline rather than on a rare branch. A
  container image scan may or may not detect the vulnerable module
  version at all, depending on whether it inspects binary buildinfo,
  but none can tell you whether the vulnerable code path is reachable,
  which is what `govulncheck`'s call-graph analysis adds.

- Raised the minimum Go version in `go.mod` from 1.26.1 to 1.26.5, which
  addresses four standard library advisories that `govulncheck` reported
  as reachable or imported: GO-2026-5856 (Encrypted Client Hello privacy
  leak in `crypto/tls`), GO-2026-5039 (unescaped input in `net/textproto`
  errors), GO-2026-5037 (inefficient candidate hostname parsing in
  `crypto/x509`), and GO-2026-5038 (`mime`). The build already floated
  on the latest 1.26 patch release, so this sets a floor that prevents a
  stale local toolchain producing a binary with a vulnerable standard
  library.

- Updated `golang.org/x/sys` from 0.26.0 to 0.44.0 for GO-2026-5024.
  That advisory affects Windows only and so did not apply to any
  supported deployment target, but the upgrade is free.

- The BM25 keyword arm no longer reads a table without a bound. Its
  query had no `LIMIT`, so every request fetched the content of every
  row matching the filter and rebuilt an in-memory index from it,
  meaning the cost of a request scaled with the size of the table rather
  than with the size of the result set. Combined with the absence of
  authentication and rate limiting on the same endpoint, that gave any
  caller a cheap way to impose sustained load on both the database and
  the server. Reads are now capped by a new per-pipeline
  `search.bm25_max_documents` option, defaulting to 10000, and requests
  that hit the cap are logged because keyword recall is then partial.
  Vector search is unaffected and continues to rank across the whole
  table via its index.

- API error responses no longer relay upstream provider error text, which
  could disclose part of the configured provider API key. The query
  handler sent the raw Go error string to the client for both ordinary
  and streaming requests, and `GET /v1/health` did the same for each
  provider's reachability error. Those errors originate in
  pgedge-go-llm-lib, which relays the provider's own JSON error body
  verbatim, and providers characteristically echo a truncated form of the
  submitted key in that body when they reject a credential. Since none
  of these endpoints is authenticated, any caller could obtain it, and in
  the health case a single unauthenticated `GET` was enough, with no
  query required.

  Client-facing messages are now a short description of the failure class
  drawn from a fixed set, produced by classifying the error rather than
  by scrubbing it: nothing derived from the provider's response body
  reaches the caller, whatever a provider chose to put there. Full detail
  is written to the server log instead, with credential-shaped strings
  removed as defence in depth. Recovered panics from a provider client
  are likewise logged rather than reported, since a panic value can carry
  anything.

  This reduces the diagnostic detail available to API clients, which is
  the point; operators should consult the server log. The `error` field
  of a health response and the `error` event of a streaming response are
  affected in the same way.

### Added

- `make vulncheck` runs `govulncheck` over the module, reporting
  vulnerabilities in dependencies and the standard library whose
  affected symbols are actually reachable from this codebase. This
  complements container image scanning rather than replacing it: an
  image scan may or may not detect a vulnerable Go module version,
  depending on whether it inspects binary buildinfo, but none can tell
  you whether the vulnerable code path is reachable.

- `disable_hybrid` on a query request skips the keyword-search arm for
  that request, using vector search alone, for callers that would rather
  have lower latency than keyword recall. It can only turn the arm off:
  a request cannot enable hybrid search where the pipeline configuration
  has disabled it, since a caller should be able to opt out of work but
  not into work an operator has declined.

- Configurable `request_timeout` and `per_attempt_timeout` for LLM
  providers. Both accept a duration string such as `90s` or `2m` and
  can be set per-pipeline or in defaults. The per-attempt timeout makes
  a slow upstream (for example a heavy embedding batch) retryable rather
  than consuming the whole request budget in one attempt.

- `GET /v1/health` now pings every pipeline's embedding and completion
  providers and reports per-pipeline connectivity in the response
  body. All providers for all pipelines are checked concurrently, so
  the check takes roughly one provider timeout regardless of how many
  pipelines are configured. An unreachable provider degrades the
  reported `status` to `"degraded"` but does not change the HTTP
  status code, so existing uptime checks that only look at HTTP 200
  keep working
  ([#23](https://github.com/pgEdge/pgedge-rag-server/issues/23)).

- `GET /v1/live` liveness endpoint: a cheap, dependency-free check
  that returns immediately and never contacts the LLM providers, so
  its latency is unaffected by provider health. Use it for a
  latency-sensitive Kubernetes liveness probe, and `/v1/health` for a
  readiness probe or monitoring
  ([#23](https://github.com/pgEdge/pgedge-rag-server/issues/23)).

### Fixed

- **Breaking:** a retrieval that could not run is now reported to the
  caller as a failure instead of as a successful search that matched
  nothing. When a configured table's vector search failed — for example
  `permission denied for table docs` (SQLSTATE 42501) — the failure was
  logged at WARN and, if any other configured table completed a lookup,
  the request still answered `200` with `No relevant information found
  in the available documents.` and `tokens_used` of `0`. A misconfigured
  or unreadable corpus was therefore indistinguishable from a genuinely
  empty one, and the only record of the real reason was the server log,
  which the person running the query usually cannot see.

  Three outcomes are now distinguished. A refused query answers `500`
  with error code `RETRIEVAL_REFUSED`: the database received the search
  and would not run it, which means a permissions or configuration
  problem for whoever deployed the pipeline, and retrying will fail
  identically. An unreachable document store answers `503` with
  `RETRIEVAL_UNAVAILABLE`, which unlike the refusal is transient and may
  be retried. A search that ran correctly and matched nothing is
  unchanged: `200`, the same answer string, the same zero token count.
  A failure of unclassified cause answers `500` with `RETRIEVAL_FAILED`.
  Streaming requests commit to `200` before retrieval starts, so for
  those the failure arrives as an `error` event on the stream.

  The error body carries no table name, schema, SQL text or SQLSTATE;
  the messages are derived from the failure classification alone, so
  nothing the database put in its own message can reach the caller. Full
  detail, including the database's message, is written to the server log
  alongside a `failure_kind` field.

  A table that fails alongside another that returns results still does
  not fail the request, since there are documents to answer from; only a
  request that ends with no results at all reports the failure. This
  supersedes the earlier carve-out where any successful lookup suppressed
  a sibling table's failure, which is the case that produced the false
  empty result above
  ([#49](https://github.com/pgEdge/pgedge-rag-server/issues/49)).

- The BM25 keyword index is now built per request instead of being a
  single per-pipeline object mutated in place. Each request used to
  clear the shared index, refill it from its own filtered documents and
  then search it; those three steps were individually locked but not
  locked as a unit, so concurrent requests interleaved and a search
  could run against a corpus that a different request's filter had
  populated. Where an application uses the `filter` parameter to scope
  results to a customer or tenant, ordinary concurrent traffic could
  therefore return rows from outside the caller's scope, with no
  malicious input involved; it also explains non-deterministic or
  simply wrong keyword results under load. Widening the lock to cover
  all three steps would have fixed correctness whilst serialising every
  request on a pipeline behind one mutex, so a per-request index was
  used instead, which is both correct and better under concurrency.

- Vector search now selects the configured `id_column`, so vector
  results carry an id. Previously the vector arm returned empty
  ids, which prevented Reciprocal Rank Fusion from merging the
  vector and BM25 arms (a document found by both was scored as
  two separate half-weight entries) and made vector results
  unusable for id-based source resolution (citations). When no
  `id_column` is configured, both search arms now fall back to
  keying on content for fusion, so a document found by both arms
  fuses into one result instead of appearing twice (and unstable
  `ROW_NUMBER()` ids no longer cause false merges)
  ([#27](https://github.com/pgEdge/pgedge-rag-server/issues/27)).

- `GET /v1/health` no longer reports a healthy provider as
  `"unreachable"` when its first ping attempt happens to need one
  ordinary retry. Ping reuses the same client (and therefore the same
  retry policy) as real requests, and the default policy backs off for
  2 seconds before a retry, which alone could consume most of a
  3-second ping budget and leave no time for the retry to complete.
  `DefaultPingTimeout` is now 10 seconds, comfortably covering one
  retry cycle
  ([#55](https://github.com/pgEdge/pgedge-rag-server/issues/55)).

### Dependencies

- Updated `pgedge-go-llm-lib` from 0.1.0 to 0.3.1. Most notably,
  `Options.WithDefaults()` no longer forces an unset per-request
  `Temperature` to a client-level default of 0.7; some newer models
  (observed: `claude-sonnet-5`) reject any temperature value outright,
  so a request built without one now actually reaches the provider
  without one. Also included: Gemini tool-calling no longer fails on a
  conversation's second turn, tool failures are now signalled to
  Gemini instead of silently retried, and `ListModels` no longer offers
  Gemini models (text-to-speech, image generation, and similar) that
  cannot hold a conversation.

- Updated `jackc/pgx/v5` from 5.9.2 to 5.10.0.

### Documentation

- Documented a set of behaviours the server already had but the
  documentation either omitted or described incorrectly. Nothing about
  the product changed; only the documentation
  ([#46](https://github.com/pgEdge/pgedge-rag-server/issues/46)).

    - The production guidance now states plainly that the server
      implements neither client authentication nor rate limiting, and
      that enabling TLS does not address either, so a production
      deployment needs an authenticating proxy in front of it. Both the
      Docker production checklist and the TLS sample configuration
      previously read as though TLS were sufficient.

    - The Docker page and the configuration reference no longer
      contradict each other over whether a configuration change needs a
      restart. The server does reload automatically, but the shipped
      `docker-compose.yml` bind-mounts the configuration as a single
      file, and because change detection watches the containing
      directory, the container never sees a host-side edit; both pages
      now say so, and the Docker page explains how to mount the
      directory instead if reloads are wanted.

    - `GET /v1/pipelines` and `GET /v1/stats` now carry the same
      unauthenticated-endpoint warning as the query and health
      endpoints: together they disclose every pipeline's name and
      description, and `/v1/stats` additionally discloses cumulative
      token consumption, to anyone who can reach the port.

    - The `ssl_cert`, `ssl_key`, and `ssl_root_ca` database settings are
      documented in the configuration reference, with an example of
      certificate-based authentication. They have worked since they were
      added but appeared nowhere in the documentation.

    - The 1 MiB cap on a query request body is documented, along with
      the `413`/`REQUEST_TOO_LARGE` response it produces, which is now
      also present in the OpenAPI specification.

    - Gemini is listed among the supported providers on the
      documentation front page, where it had been missed despite being
      fully supported and documented elsewhere.

    - The configuration reference notes that `target_session_attrs` is
      rejected at startup unless `hosts` is also set, which matters
      because the adjacent section invites readers to keep a
      single-host configuration.

## [1.0.0] - 2026-04-04

### Added

- Multi-host database connection support for HA deployments.
  Configure multiple database hosts for automatic failover
  ([#13](https://github.com/pgEdge/pgedge-rag-server/pull/13)).

- Minimum similarity threshold to avoid sending irrelevant
  documents to the LLM. Set `minimum_similarity` per-pipeline
  to filter out low-quality search results.

- Configurable `base_url` for LLM providers (Anthropic, OpenAI,
  Voyage AI). This allows routing requests through API gateways
  such as [Portkey](https://portkey.ai) or custom proxies. The
  `base_url` can be set per-pipeline or in defaults and follows
  the same inheritance rules as `provider` and `model`
  ([#7](https://github.com/pgEdge/pgedge-rag-server/issues/7)).

- `vector_weight` parameter for tuning Reciprocal Rank Fusion
  (RRF) scoring between vector and text search results.

- View support for database sources, allowing pipelines to
  query from database views in addition to tables.

- Hybrid search control options for fine-tuning search behavior.

### Improved

- OpenAPI specification: added `enum` constraints for
  `Filter.logic` (`AND`, `OR`) and `FilterCondition.operator`
  (all 12 valid operators), and `maxItems: 50` on
  `Filter.conditions` to limit payload size and improve
  client-side validation and code generation.

### Fixed

- Guard against nil dereference when vector weight is unset.
- Skip zero-weight loops in RRF scoring to avoid unnecessary
  computation.

### Security

- Pinned GitHub Actions to commit SHAs to prevent supply chain
  attacks.
- Upgraded Go to 1.25.8 and updated dependencies to resolve
  security findings.

## [1.0.0-beta3] - 2026-01-26

### Added

- CORS (Cross-Origin Resource Sharing) support for browser-based API access.
  Configure with `server.cors.enabled` and `server.cors.allowed_origins` to
  allow web applications to make requests from different origins.

- Configurable system prompt per pipeline. Use the `system_prompt` field in
  pipeline configuration to customize LLM instructions for generating responses.

### Fixed

- Docker image now includes CA certificates for HTTPS connections to LLM APIs.

### Documentation

- Added quickstart demo guide with step-by-step instructions.
- Fixed typos and incorrect version references in installation documentation.

## [1.0.0-beta2] - 2025-12-17

### Added

- Docker workflow for building and publishing container images
- Release badge added to README
- Manual trigger support for Docker builds from specific tags

## [1.0.0-beta1] - 2025-12-15

### Changed

- Documentation restructured for web publishing at docs.pgedge.com
- New documentation pages: installation guide, quickstart tutorial, usage
  reference, API keys management, and sample configurations
- Improved navigation and organization of documentation

## [1.0.0-alpha5] - 2025-12-03

### Fixed

- Fixed filter parameter indexing bug where API filters would cause
  "operator does not exist: text = vector" errors. The filter clause
  was generating SQL placeholders starting at `$1`, but `VectorSearch`
  already uses `$1` for the vector and `$2` for LIMIT.

## [1.0.0-alpha4] - 2025-12-03

### Breaking Changes

- **Security Fix**: API filter parameters now require structured filter format
  to eliminate SQL injection vulnerabilities. API filters must use conditions,
  operators, and values (parameterized queries prevent injection).

- **Config Rename**: The `column_pairs` field in pipeline configuration has
  been renamed to `tables` for clarity. The internal type `ColumnPair` is now
  `TableSource`.

### Changed

- Filter system now uses parameterized queries for API request filters
- API `filter` parameter changed from string to structured object
- Config `filter` field now accepts either raw SQL strings (for complex
  queries like subqueries) or structured filter objects

### Added

- Config filters support raw SQL strings for complex expressions (subqueries,
  JOINs, functions) that cannot be expressed with structured format. Since
  config files are admin-controlled, raw SQL is safe here.

### Migration Guide

**API filters (JSON) - must use structured format:**

Old (removed):

```json
{"filter": "product = 'pgAdmin'"}
```

New:

```json
{
  "filter": {
    "conditions": [
      {"column": "product", "operator": "=", "value": "pgAdmin"}
    ]
  }
}
```

**Config filters (YAML) - both formats supported:**

Raw SQL (for complex queries):

```yaml
filter: "source_id IN (SELECT id FROM sources WHERE product='pgEdge')"
```

Structured:

```yaml
filter:
  conditions:
    - column: "product"
      operator: "="
      value: "pgAdmin"
    - column: "status"
      operator: "="
      value: "published"
  logic: "AND"
```

**Supported operators (for structured filters):** `=`, `!=`, `<`, `>`, `<=`,
`>=`, `LIKE`, `ILIKE`, `IN`, `NOT IN`, `IS NULL`, `IS NOT NULL`

**Config field rename:**

Old:

```yaml
column_pairs:
  - table: "documents"
    text_column: "content"
    vector_column: "embedding"
```

New:

```yaml
tables:
  - table: "documents"
    text_column: "content"
    vector_column: "embedding"
```

## [1.0.0-alpha3] - 2025-12-01

### Added

- GitHub Actions workflow to generate builds when repository is tagged,
  producing binaries for multiple platforms (linux/amd64, linux/arm64,
  darwin/amd64, darwin/arm64).

## [1.0.0-alpha2] - 2025-12-01

### Added

- Per-pipeline LLM configuration: Pipelines can override `embedding_llm`
  and `rag_llm` settings, allowing different pipelines to use different
  providers or models.

- Per-pipeline API keys: API keys can be configured at three levels with
  cascade priority (pipeline > defaults > global), enabling different
  pipelines to use separate API keys or accounts.

- SQL filter support: Tables can include a `filter` field with a
  SQL WHERE clause fragment applied to all queries. Filters can also be
  specified per-request via the API.

- Extended defaults section: The `defaults` configuration now supports
  `embedding_llm`, `rag_llm`, and `api_keys` in addition to `token_budget`
  and `top_n`.

## [1.0.0-alpha1] - 2025-11-28

### Added

- Initial RAG server implementation with REST API
- Multiple pipeline support for different data sources
- Hybrid search combining vector similarity (pgvector) and BM25 text matching
- Reciprocal Rank Fusion (RRF) for combining search results
- Support for multiple LLM providers:

    - OpenAI (embeddings and completions)
    - Anthropic (completions)
    - Voyage AI (embeddings)
    - Ollama (embeddings and completions)

- Token budget management to control LLM costs
- Streaming responses via Server-Sent Events (SSE)
- TLS/HTTPS support for production deployments
- OpenAPI v3 specification with RFC 8631 Link headers
- Flexible API key configuration (files, environment variables, defaults)
- Comprehensive test coverage for core modules