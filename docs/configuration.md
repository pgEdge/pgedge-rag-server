# Configuration Reference

The pgEdge RAG Server is configured using a YAML file. This document describes
all available configuration options.

## Command Line Options

```bash
./bin/pgedge-rag-server [options]
```

| Option     | Description                               |
|------------|-------------------------------------------|
| `-config`  | Path to configuration file (see below)    |
| `-openapi` | Output OpenAPI v3 specification and exit  |
| `-version` | Show version information and exit         |
| `-help`    | Show help message and exit                |

If `-config` is not specified, the server searches for configuration files in:

1. `/etc/pgedge/pgedge-rag-server.yaml`
2. `pgedge-rag-server.yaml` (in the binary's directory)

## Configuration File Structure

The configuration file has the following top-level sections:

- `server` - HTTP/HTTPS server settings
- `api_keys` - Optional paths to API key files
- `pipelines` - RAG pipeline definitions

## Server Configuration

```yaml
server:
  listen_address: "0.0.0.0"
  port: 8080
  tls:
    enabled: true
    cert_file: "/path/to/cert.pem"
    key_file: "/path/to/key.pem"
```

| Field            | Description                        | Default       |
|------------------|------------------------------------|---------------|
| `listen_address` | IP address to bind to              | `0.0.0.0`     |
| `port`           | Port to listen on                  | `8080`        |
| `tls.enabled`    | Enable TLS/HTTPS                   | `false`       |
| `tls.cert_file`  | Path to TLS certificate            | Required if TLS enabled |
| `tls.key_file`   | Path to TLS private key            | Required if TLS enabled |

## Pipeline Configuration

Each pipeline defines a RAG search configuration with its own database,
embedding provider, and completion provider.

```yaml
pipelines:
  - name: "my-docs"
    description: "Search my documentation"
    database:
      host: "localhost"
      port: 5432
      database: "mydb"
      username: "postgres"
      password: ""
      ssl_mode: "prefer"
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
    token_budget: 4000
    top_n: 10
```

### Pipeline Fields

| Field          | Description                                    | Required |
|----------------|------------------------------------------------|----------|
| `name`         | Unique pipeline identifier (used in API URLs)  | Yes      |
| `description`  | Human-readable description                     | No       |
| `database`     | PostgreSQL connection settings                 | Yes      |
| `column_pairs` | Tables and columns to search                   | Yes      |
| `embedding_llm`| Embedding provider configuration               | Yes      |
| `rag_llm`      | Completion provider configuration              | Yes      |
| `token_budget` | Maximum tokens for context documents           | No (default: 4000) |
| `top_n`        | Maximum number of results to retrieve          | No (default: 10) |

### Database Fields

| Field      | Description                              | Default    |
|------------|------------------------------------------|------------|
| `host`     | PostgreSQL host                          | `localhost`|
| `port`     | PostgreSQL port                          | `5432`     |
| `database` | Database name                            | Required   |
| `username` | Database user                            | `postgres` |
| `password` | Database password                        | `""`       |
| `ssl_mode` | SSL mode (disable, allow, prefer, etc.)  | `prefer`   |

### Column Pair Fields

Each column pair specifies a table with text content and its corresponding
vector embeddings.

| Field           | Description                          | Required |
|-----------------|--------------------------------------|----------|
| `table`         | Table name                           | Yes      |
| `text_column`   | Column containing text content       | Yes      |
| `vector_column` | Column containing vector embeddings  | Yes      |
| `filter`        | SQL WHERE clause to filter results   | No       |

The `filter` field allows you to specify a SQL WHERE clause fragment that
will be applied to all queries for this column pair. For example:

```yaml
column_pairs:
  - table: "documents"
    text_column: "content"
    vector_column: "embedding"
    filter: "product = 'pgAdmin' AND status = 'published'"
```

Filters can also be specified per-request via the API's `filter` parameter,
which will be combined with any configured filter using AND.

### LLM Provider Configuration

Both `embedding_llm` and `rag_llm` use the same configuration structure:

| Field      | Description                  | Required |
|------------|------------------------------|----------|
| `provider` | LLM provider name            | Yes      |
| `model`    | Model name                   | Yes      |

#### Supported Providers

| Provider    | Embedding Support | Completion Support |
|-------------|-------------------|-------------------|
| `openai`    | Yes               | Yes               |
| `anthropic` | No*               | Yes               |
| `voyage`    | Yes               | No                |
| `ollama`    | Yes               | Yes               |

*Anthropic does not provide embedding models; use OpenAI or Voyage for
embeddings with Anthropic for completions.

## API Keys

API keys are loaded using the following priority order:

1. **Configuration file paths** (if specified in `api_keys` section)
2. **Environment variables**
3. **Default file locations** in your home directory

### Configuration File Paths

You can specify paths to files containing API keys:

```yaml
api_keys:
  anthropic: "/etc/pgedge/keys/anthropic.key"
  voyage: "/etc/pgedge/keys/voyage.key"
  openai: "~/secrets/openai-api-key"
```

| Field       | Description                           |
|-------------|---------------------------------------|
| `anthropic` | Path to file containing Anthropic key |
| `openai`    | Path to file containing OpenAI key    |
| `voyage`    | Path to file containing Voyage key    |

Paths support `~` expansion for the home directory. Each file should contain
only the API key (no other content).

### Environment Variables

If no configuration file path is specified, the server checks environment
variables:

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export VOYAGE_API_KEY="pa-..."
```

### Default File Locations

If neither configuration paths nor environment variables are set, the server
looks for API keys in these default locations:

| Provider  | File Location           |
|-----------|-------------------------|
| OpenAI    | `~/.openai-api-key`     |
| Anthropic | `~/.anthropic-api-key`  |
| Voyage    | `~/.voyage-api-key`     |

## Ollama Configuration

Ollama runs locally and does not require API keys. By default, it connects to
`http://localhost:11434`. To use a different URL, set the `OLLAMA_HOST`
environment variable:

```bash
export OLLAMA_HOST="http://my-ollama-server:11434"
```

## Example Configurations

### Minimal Configuration

```yaml
pipelines:
  - name: "docs"
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

### Production Configuration with TLS

```yaml
server:
  listen_address: "0.0.0.0"
  port: 443
  tls:
    enabled: true
    cert_file: "/etc/ssl/certs/server.pem"
    key_file: "/etc/ssl/private/server.key"

pipelines:
  - name: "knowledge-base"
    description: "Corporate knowledge base search"
    database:
      host: "db.example.com"
      port: 5432
      database: "knowledge"
      username: "rag_user"
      ssl_mode: "require"
    column_pairs:
      - table: "articles"
        text_column: "body"
        vector_column: "embedding"
      - table: "faqs"
        text_column: "answer"
        vector_column: "answer_embedding"
    embedding_llm:
      provider: "voyage"
      model: "voyage-3"
    rag_llm:
      provider: "anthropic"
      model: "claude-sonnet-4-20250514"
    token_budget: 8000
    top_n: 15
```

### Local Development with Ollama

```yaml
pipelines:
  - name: "local-docs"
    description: "Local document search"
    database:
      host: "localhost"
      database: "devdb"
    column_pairs:
      - table: "docs"
        text_column: "content"
        vector_column: "embedding"
    embedding_llm:
      provider: "ollama"
      model: "nomic-embed-text"
    rag_llm:
      provider: "ollama"
      model: "llama3.2"
    token_budget: 2000
    top_n: 5
```

### Voyage Embeddings with Anthropic Completion

This configuration uses Voyage for high-quality embeddings and Anthropic
Claude for completions, with API keys stored in external files:

```yaml
api_keys:
  voyage: "/etc/pgedge/keys/voyage.key"
  anthropic: "/etc/pgedge/keys/anthropic.key"

pipelines:
  - name: "enterprise-search"
    description: "Enterprise document search with Voyage and Claude"
    database:
      host: "db.internal"
      port: 5432
      database: "documents"
      username: "rag_service"
      ssl_mode: "require"
    column_pairs:
      - table: "knowledge_base"
        text_column: "content"
        vector_column: "embedding"
    embedding_llm:
      provider: "voyage"
      model: "voyage-3"
    rag_llm:
      provider: "anthropic"
      model: "claude-sonnet-4-20250514"
    token_budget: 8000
    top_n: 10
```
