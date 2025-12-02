# Changelog

All notable changes to the pgEdge RAG Server will be
documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

- SQL filter support: Column pairs can include a `filter` field with a
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