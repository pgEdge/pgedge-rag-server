# pgEdge RAG Server

[![CI](https://github.com/pgEdge/pgedge-rag-server/actions/workflows/ci.yml/badge.svg)](https://github.com/pgEdge/pgedge-rag-server/actions/workflows/ci.yml) [![Release](https://github.com/pgEdge/pgedge-rag-server/actions/workflows/release.yml/badge.svg)](https://github.com/pgEdge/pgedge-rag-server/actions/workflows/release.yml)

  - [pgEdge RAG Server](docs/index.md)
  - [Architecture](docs/architecture.md)
  - Installing pgEdge RAG Server
      - [Installing pgEdge RAG Server](docs/installation.md)
      - [pgEdge RAG Server Quickstart](docs/quickstart.md)
  - Configuring the pgEdge RAG Server
      - [Creating a Configuration File](docs/configuration.md)
      - [Sample Configuration Files](docs/samples.md)
      - [Managing API Keys](docs/keys.md)
  - [Using pgEdge RAG Server](docs/usage.md)
  - Using the RAG Server API
      - [API Reference](docs/api/reference.md)
      - [Using the API in a browser](docs/api/browser.md)
  - [pgEdge RAG Server Release Notes](docs/changelog.md)
  - [Developer Notes](docs/development.md)
  - [Licence](docs/LICENCE.md)

The pgEdge RAG Server is a simple API server for performing Retrieval-Augmented Generation (RAG) of text based on content from a PostgreSQL database using [pgvector](https://github.com/pgvector/pgvector).

Documentation for the RAG Server is available online at: [https://docs.pgedge.com/pgedge-rag-server/](https://docs.pgedge.com/pgedge-rag-server/)

The RAG Server features:

- Multiple RAG pipelines with configurable embedding and LLM providers
- Hybrid search combining vector similarity and BM25 text matching
- Support for OpenAI, Anthropic, Voyage, and Ollama LLM providers
- Token budget management to control LLM costs
- Optional streaming responses via Server-Sent Events
- TLS/HTTPS support


## Quick Start - Using the RAG Server

To use the pgEdge RAG Server, you must:

1. [Build](#building-from-source) the pgedge-rag-server binary.
2. [Create a configuration file](#creating-a-configuration-file) that specifies details used by the RAG server.
3. Invoke pgedge-rag-server.


Before installing pgEdge RAG Server, you should install or obtain:

- Go 1.22 or later
- PostgreSQL 14 or later, with [pgvector installed](https://github.com/pgvector/pgvector)
- [API keys](docs/keys.md) for your chosen LLM providers

### Building from Source

Before building the binary, clone the RAG server repository and navigate into the root of the repo:

```bash
git clone https://github.com/pgedge/pgedge-rag-server.git
cd pgedge-rag-server
```

Build the pgEdge RAG server binary with the command; the binary is created in the bin directory:

```bash
make build
```

After installation, verify the tool is working:

```bash
pgedge-rag-server version
```

You can also access online help after building RAG server:

```bash
pgedge-rag-server help
```

### Creating a Configuration File

Create a configuration file that specifies server connection details and other properties; (see [the online documentation](docs/configuration.md) for complete details.  The default name of the file is `pgedge-rag-server.yaml`; when invoked, the server searches for configuration file in:

1. `/etc/pgedge/pgedge-rag-server.yaml`
2. the directory that contains the `pgedge-rag-server` binary.

You can optionally use the `-config` option on the command line to specify the complete path to a custom location for the configuration file.

The following sample demonstrates a minimal configuration:

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
    tables:
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

### Invoking the RAG Server

After building the binary and creating a configuration file, you can invoke `pgedge-rag-server`.  Use the command:

```bash
./bin/pgedge-rag-server (options)
```

You can include the following options when invoking the server:

| Option     | Description                               |
|------------|-------------------------------------------|
| `-config`  | Path to configuration file (see below)    |
| `-openapi` | Output OpenAPI v3 specification and exit  |
| `-version` | Show version information and exit         |
| `-help`    | Show help message and exit                |

When you invoke `pgedge-rag-server` you can optionally include the `-config` option to specify the complete path to a custom location for the configuration file.  If you do not specify a location on the command line, the server searches for configuration files in:

1. `/etc/pgedge/pgedge-rag-server.yaml`
2. `pgedge-rag-server.yaml` (in the binary's directory)


## API Usage Reference

The online documentation contains detailed information about [using the API](docs/api/reference.md), and allows you to try the [API in a browser](docs/api/browser.md).

**To List Available Pipelines**

```bash
curl http://localhost:8080/v1/pipelines
```

**To Query a Pipeline**

```bash
curl -X POST http://localhost:8080/v1/pipelines/my-docs \
  -H "Content-Type: application/json" \
  -d '{"query": "How do I configure replication?"}'
```

**To Query with Streaming**

```bash
curl -X POST http://localhost:8080/v1/pipelines/my-docs \
  -H "Content-Type: application/json" \
  -d '{"query": "How do I configure replication?", "stream": true}'
```


## License

This project is licensed under the [PostgreSQL License](docs/LICENCE.md).
