# pgEdge RAG Server

[![CI](https://github.com/pgEdge/pgedge-rag-server/actions/workflows/ci.yml/badge.svg)](https://github.com/pgEdge/pgedge-rag-server/actions/workflows/ci.yml)

A simple API server for performing Retrieval-Augmented Generation (RAG) of
text based on content from a PostgreSQL database using
[pgvector](https://github.com/pgvector/pgvector).

## Features

- Multiple RAG pipelines with configurable embedding and LLM providers
- Hybrid search combining vector similarity and BM25 text matching
- Support for OpenAI, Anthropic, Voyage, and Ollama LLM providers
- Token budget management to control LLM costs
- Optional streaming responses via Server-Sent Events
- TLS/HTTPS support

## Quick Start

### Prerequisites

- Go 1.22 or later
- PostgreSQL with pgvector extension
- API keys for your chosen LLM providers

### Installation

```bash
# Clone the repository
git clone https://github.com/pgEdge/pgedge-rag-server.git
cd pgedge-rag-server

# Build the binary
make build

# Run the server
./bin/pgedge-rag-server -config /path/to/config.yaml
```

### Configuration

Create a configuration file (see [docs/configuration.md](docs/configuration.md)
for full reference):

```yaml
server:
  listen_address: "0.0.0.0"
  port: 8080

pipelines:
  - name: "my-docs"
    description: "Search my documentation"
    database:
      host: "localhost"
      port: 5432
      database: "mydb"
    column_pairs:
      - table: "documents"
        text_column: "content"
        vector_column: "embedding"
    embedding_llm:
      provider: "openai"
      model: "text-embedding-3-small"
    rag_llm:
      provider: "anthropic"
      model: "claude-sonnet-4-20250514"
```

## API Usage

### List Available Pipelines

```bash
curl http://localhost:8080/v1/pipelines
```

### Query a Pipeline

```bash
curl -X POST http://localhost:8080/v1/pipelines/my-docs \
  -H "Content-Type: application/json" \
  -d '{"query": "How do I configure replication?"}'
```

### Query with Streaming

```bash
curl -X POST http://localhost:8080/v1/pipelines/my-docs \
  -H "Content-Type: application/json" \
  -d '{"query": "How do I configure replication?", "stream": true}'
```

## Development

```bash
# Run all checks (format, lint, test, build)
make all

# Run tests only
make test

# Run linter only
make lint

# Format code
make fmt
```

## Documentation

Full documentation is available in the [docs/](docs/) directory or at
[https://pgedge.github.io/pgedge-rag-server](https://pgedge.github.io/pgedge-rag-server).

## License

See [LICENCE.md](LICENCE.md) for license information.
