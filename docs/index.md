# pgEdge RAG Server

A simple API server for performing Retrieval-Augmented Generation (RAG)
of text based on content from a PostgreSQL database using
[pgvector](https://github.com/pgvector/pgvector).

## What is RAG?

Retrieval-Augmented Generation combines information retrieval with
generative AI to produce accurate, grounded responses. Instead of relying
solely on an LLM's training data, RAG:

1. Retrieves relevant documents from a knowledge base
2. Provides those documents as context to the LLM
3. Generates an answer based on the retrieved information

This approach reduces hallucinations and keeps responses current with your
data.

## Features

- **Multiple Pipelines** - Configure separate RAG pipelines for different
  data sources, each with its own database, embedding model, and LLM

- **Hybrid Search** - Combines vector similarity (semantic) and BM25
  (keyword) search using Reciprocal Rank Fusion for better results

- **Multiple LLM Providers** - Support for OpenAI, Anthropic, Voyage, and
  Ollama

- **Token Budget Management** - Automatically manages context size to
  control LLM costs

- **Streaming Responses** - Optional real-time streaming via Server-Sent
  Events

- **TLS Support** - Built-in HTTPS support for production deployments

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
./bin/pgedge-rag-server -config config.yaml
```

### Basic Configuration

Create a `config.yaml` file:

```yaml
pipelines:
  - name: "my-docs"
    description: "Search my documentation"
    database:
      host: "localhost"
      database: "mydb"
    column_pairs:
      - table: "documents"
        text_column: "content"
        vector_column: "embedding"
    embedding_llm:
      provider: "openai"
      model: "text-embedding-3-small"
    rag_llm:
      provider: "openai"
      model: "gpt-4o-mini"
```

Set your API key:

```bash
export OPENAI_API_KEY="sk-..."
```

### Query the Server

```bash
# List available pipelines
curl http://localhost:8080/v1/pipelines

# Ask a question
curl -X POST http://localhost:8080/v1/pipelines/my-docs \
  -H "Content-Type: application/json" \
  -d '{"query": "How do I get started?"}'
```

## Documentation

- [Configuration Reference](configuration.md) - Complete configuration
  options
- [API Reference](api/reference.md) - REST API documentation
- [Architecture](architecture.md) - How the server works internally

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

## License

See [LICENSE.md](https://github.com/pgEdge/pgedge-rag-server/blob/main/LICENCE.md)
for license information.
