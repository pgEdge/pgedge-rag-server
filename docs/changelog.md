# Changelog

All notable changes to the pgEdge RAG Server will be
documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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