# Configuration Reference

Before invoking the [`pgedge-rag-server` executable](usage.md), you need to create a .YAML file that contains the deployment details for the RAG server.  These details include:

* connection information.
* AI provider properties.
* [API key location](keys.md).
* embedding information for your Postgres database.

The default name of the file is `pgedge-rag-server.yaml`.

When you invoke `pgedge-rag-server` you can optionally include the `--config` option to specify the complete path to a custom location for the configuration file.  If you do not specify a location on the command line, the server searches for configuration files in:

1. `/etc/pgedge/pgedge-rag-server.yaml`
2. the directory that contains the `pgedge-rag-server` binary.


## Configuration File Structure

The configuration file includes the following top-level sections:

- [`server`](#specifying-properties-in-the-server-section) - HTTP/HTTPS server settings
- [`defaults`](#specifying-properties-in-the-defaults-section) - Default values for pipelines (LLM providers, token budget, etc.)
- [`pipelines`](#specifying-properties-in-the-server-section) - RAG pipeline definitions

You can optionally [set the API key value](keys.md) in the configuration file, on the command line, or in an environment variable.

## Specifying Properties in the Server Section

Use the properties shown below to specify connection properties for your RAG server:

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


## Specifying Properties in the Defaults Section

The `defaults` section allows you to set default values for LLM providers, API keys, and other settings that can be overridden per-pipeline. This is useful when most pipelines share the same configuration.

```yaml
defaults:
  token_budget: 4000
  top_n: 10
  embedding_llm:
    provider: "openai"
    model: "text-embedding-3-small"
  rag_llm:
    provider: "anthropic"
    model: "claude-sonnet-4-20250514"
  api_keys:
    openai: "/etc/pgedge/keys/openai.key"
    anthropic: "/etc/pgedge/keys/anthropic.key"
```

| Field            | Description                              | Default |
|------------------|------------------------------------------|---------|
| `token_budget`   | Default token budget for context         | `4000`  |
| `top_n`          | Default number of results to retrieve    | `10`    |
| `embedding_llm`  | Default embedding provider configuration | None    |
| `rag_llm`        | Default completion provider configuration| None    |
| `api_keys`       | Default API key file paths               | None    |

When you set default values, your individual pipelines definitions can omit the corresponding fields and will inherit the default values. A Pipeline can also override specific fields while inheriting others.

## Specifying Properties in the Pipeline Section

Each pipeline defines a RAG search configuration with its own database, embedding provider, and completion provider.  Use the properties in the sections that follow to provide information in the `pipelines` section:

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
    token_budget: 4000
    top_n: 10
```

### Pipeline Properties

| Field           | Description                                                  | Required |
|-----------------|--------------------------------------------------------------|----------|
| `name`          | Unique pipeline identifier (used in API URLs)                | Yes      |
| `description`   | Human-readable description                                   | No       |
| `database`      | [PostgreSQL connection settings](#database-properties)       | Yes      |
| `tables`        | [Tables and columns to search](#table-properties)            | Yes      |
| `embedding_llm` | [Embedding provider configuration](#llm-provider-properties) | Yes (unless set in defaults) |
| `rag_llm`       | Completion provider configuration                            | Yes (unless set in defaults) |
| `api_keys`      | API key file paths (overrides defaults/global)               | No       |
| `token_budget`  | Maximum tokens for context documents                         | No (uses defaults) |
| `top_n`         | Maximum number of results to retrieve                        | No (uses defaults) |

### Database Properties

| Field      | Description                              | Default    |
|------------|------------------------------------------|------------|
| `host`     | PostgreSQL host                          | `localhost`|
| `port`     | PostgreSQL port                          | `5432`     |
| `database` | Database name                            | Required   |
| `username` | Database user                            | `postgres` |
| `password` | Database password                        | `""`       |
| `ssl_mode` | SSL mode (disable, allow, prefer, etc.)  | `prefer`   |

### Table Properties

Each table entry specifies a table with text content and its corresponding vector embeddings.

| Field           | Description                          | Required |
|-----------------|--------------------------------------|----------|
| `table`         | Table name                           | Yes      |
| `text_column`   | Column containing text content       | Yes      |
| `vector_column` | Column containing vector embeddings  | Yes      |
| `filter`        | Filter to apply to results           | No       |

The `filter` field allows you to specify a filter that will be applied to all
queries for this table. It can be specified in two formats:

**Raw SQL (for complex queries like subqueries):**

```yaml
tables:
  - table: "documents"
    text_column: "content"
    vector_column: "embedding"
    filter: "source_id IN (SELECT id FROM sources WHERE product='pgEdge')"
```

**Structured filter (using conditions):**

```yaml
tables:
  - table: "documents"
    text_column: "content"
    vector_column: "embedding"
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

Raw SQL filters are useful when you need complex expressions like subqueries, JOINs, or functions that cannot be expressed with the structured format. Since config files are controlled by administrators, raw SQL is safe to use here.

Filters can also be specified per-request via the API's `filter` parameter. API filters must use the structured format (for security) and will be combined with any configured filter using AND.

**Supported operators (for structured filters):** `=`, `!=`, `<`, `>`, `<=`,
`>=`, `LIKE`, `ILIKE`, `IN`, `NOT IN`, `IS NULL`, `IS NOT NULL`

### LLM Provider Properties

The `embedding_llm` and `rag_llm` properties use the same configuration structure:

| Field      | Description                  | Required |
|------------|------------------------------|----------|
| `provider` | LLM provider name            | Yes      |
| `model`    | Model name                   | Yes      |

The RAG server supports the following providers:

| Provider    | Embedding Support | Completion Support |
|-------------|-------------------|-------------------|
| `openai`    | Yes               | Yes               |
| `anthropic` | No*               | Yes               |
| `voyage`    | Yes               | No                |
| `ollama`    | Yes               | Yes               |

Anthropic does not provide embedding models; use OpenAI or Voyage for
embeddings with Anthropic for completions.


