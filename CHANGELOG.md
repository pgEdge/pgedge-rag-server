# Changelog

All notable changes to the pgEdge RAG Server will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0-alpha2] - 2025-12-01

### Added

- Per-pipeline LLM configuration: Pipelines can now override `embedding_llm`
  and `rag_llm` settings from defaults, allowing different pipelines to use
  different providers or models.

- Per-pipeline API keys: API keys can be configured at three levels with
  cascade priority (pipeline > defaults > global), enabling different
  pipelines to use different API keys or accounts.

- SQL filter support: Column pairs can now include a `filter` field with a
  SQL WHERE clause fragment that is applied to all queries for that column
  pair. Filters can also be specified per-request via the API.

- Defaults section in configuration: The `defaults` section now supports
  `embedding_llm`, `rag_llm`, and `api_keys` in addition to `token_budget`
  and `top_n`.

### Changed

- API key loading is now performed per-pipeline rather than globally,
  allowing each pipeline to use its own set of API keys.

## [1.0.0-alpha1] - 2025-11-30

### Added

- Initial release of pgEdge RAG Server.
- Multi-pipeline support with independent database connections.
- Vector similarity search using pgvector.
- BM25 text search for hybrid retrieval.
- Support for OpenAI, Anthropic, Voyage, and Ollama LLM providers.
- Streaming and non-streaming response modes.
- OpenAPI v3 specification endpoint.
- TLS/HTTPS support.
- Configurable token budgets and result limits.
