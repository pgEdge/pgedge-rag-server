# Configuration Reference

Before invoking the [`pgedge-rag-server` executable](usage.md), you need to create a .YAML file that contains the deployment details for the RAG server.  These details include:

* connection information.
* AI provider properties.
* [API key location](keys.md).
* embedding information for your Postgres database.

The default name of the file is `pgedge-rag-server.yaml`.

When you invoke `pgedge-rag-server` you can optionally include the `-config` option to specify the complete path to a custom location for the configuration file.  If you do not specify a location on the command line, the server searches for configuration files in:

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
  cors:
    enabled: true
    allowed_origins:
      - "http://localhost:3000"
      - "https://myapp.example.com"
```

| Field                  | Description                        | Default       |
|------------------------|------------------------------------|---------------|
| `listen_address`       | IP address to bind to              | `0.0.0.0`     |
| `port`                 | Port to listen on                  | `8080`        |
| `tls.enabled`          | Enable TLS/HTTPS                   | `false`       |
| `tls.cert_file`        | Path to TLS certificate            | Required if TLS enabled |
| `tls.key_file`         | Path to TLS private key            | Required if TLS enabled |
| `cors.enabled`         | Enable CORS headers                | `false`       |
| `cors.allowed_origins` | List of allowed origins            | `[]` (none)   |

### CORS Configuration

CORS (Cross-Origin Resource Sharing) allows browser-based applications to make
requests to the RAG server from different origins. Enable CORS when you need to
access the API from web applications hosted on different domains.

To allow all origins, use a wildcard:

```yaml
server:
  cors:
    enabled: true
    allowed_origins:
      - "*"
```

For production, specify exact origins for better security:

```yaml
server:
  cors:
    enabled: true
    allowed_origins:
      - "https://myapp.example.com"
      - "https://docs.example.com"
```


## Specifying Properties in the Defaults Section

The `defaults` section allows you to set default values for LLM providers, API keys, and other settings that can be overridden per-pipeline. This is useful when most pipelines share the same configuration.

```yaml
defaults:
  token_budget: 4000
  top_n: 10
  embedding_llm:
    provider: "openai"
    model: "text-embedding-3-small"
    base_url: "https://gateway.example.com/v1"
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

The token budget prevents sending too much context to the LLM; this ensures predictable LLM costs while maximizing relevant context.  The [orchestrator](architecture.md):

1. Estimates tokens for each document (approximately 4 characters per token).
2. Includes documents until the budget is reached.
3. Truncates the final document at a sentence boundary if it exceeds the remaining budget.

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
| `system_prompt` | Custom system prompt for the LLM                             | No (uses default) |

### System Prompt

The `system_prompt` field allows you to customize the instructions given to the
LLM for generating responses. If not specified, the following default is used:

```
You are a helpful assistant that answers questions based on the provided context.
Answer the question using only the information from the context.
If the context doesn't contain enough information to answer, say so.
Be concise and accurate in your responses.
```

Example with custom system prompt:

```yaml
pipelines:
  - name: "support-docs"
    system_prompt: |
      You are a technical support assistant for our product.
      Answer questions based only on the provided documentation.
      If you cannot find the answer in the context, suggest contacting support.
      Use a friendly, professional tone.
```

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

Each table entry specifies a table with text content and its corresponding vector embeddings.  Each table used in a pipeline must have:

- A text column containing the document content
- A vector column containing the embedding (using pgvector)


| Field           | Description                          | Required |
|-----------------|--------------------------------------|----------|
| `table`         | Table name (or view name)            | Yes      |
| `text_column`   | Column containing text content       | Yes      |
| `vector_column` | Column containing vector embeddings  | Yes      |
| `id_column`     | Column to use as document ID         | No*      |
| `filter`        | Filter to apply to results           | No       |

*The `id_column` is required when using views, as views don't have a `ctid`
system column. For regular tables, it's optional but recommended for stable
document identification in hybrid search results.

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

The `embedding_llm` and `rag_llm` properties use the same
configuration structure:

| Field      | Description                  | Required |
|------------|------------------------------|----------|
| `provider` | LLM provider name            | Yes      |
| `model`    | Model name                   | Yes      |
| `base_url` | Custom API base URL          | No       |

The optional `base_url` field allows you to route requests
through an API gateway (such as [Portkey](https://portkey.ai))
or a custom proxy. When not specified, each provider uses its
default URL:

| Provider    | Default Base URL                    |
|-------------|-------------------------------------|
| `openai`    | `https://api.openai.com/v1`         |
| `anthropic` | `https://api.anthropic.com/v1`      |
| `voyage`    | `https://api.voyageai.com/v1`       |
| `ollama`    | `http://localhost:11434`            |

Example with a custom base URL:

```yaml
embedding_llm:
  provider: "openai"
  model: "text-embedding-3-small"
  base_url: "https://gateway.example.com/v1"
rag_llm:
  provider: "anthropic"
  model: "claude-sonnet-4-20250514"
  base_url: "https://gateway.example.com/anthropic"
```

The `base_url` can also be set in the `defaults` section and
will be inherited by pipelines that don't specify their own.

The RAG server supports the following providers:

| Provider    | Embedding Support | Completion Support |
|-------------|-------------------|-------------------|
| `openai`    | Yes               | Yes               |
| `anthropic` | No*               | Yes               |
| `voyage`    | Yes               | No                |
| `ollama`    | Yes               | Yes               |

Anthropic does not provide embedding models; use OpenAI or Voyage for
embeddings with Anthropic for completions.

### Search Configuration

The `search` section controls how the RAG server performs document retrieval.
By default, hybrid search (combining vector similarity and BM25 keyword
matching) is enabled.

```yaml
pipelines:
  - name: "my-docs"
    # ... other config ...
    search:
      hybrid_enabled: true
      vector_weight: 0.7
```

| Field            | Description                              | Default |
|------------------|------------------------------------------|---------|
| `hybrid_enabled` | Enable hybrid search (vector + BM25)     | `true`  |
| `vector_weight`  | Weight for vector vs BM25 (0.0 to 1.0)   | `0.5`   |

**Understanding vector_weight:**

- `1.0` = Pure vector search (BM25 disabled)
- `0.5` = Equal weight to vector and BM25 results
- `0.0` = Pure BM25 search (not recommended)

**Disabling hybrid search:**

To use only vector search (no BM25), you can either:

1. Set `hybrid_enabled: false`
2. Set `vector_weight: 1.0`

Both approaches skip the BM25 search phase entirely.

**When to adjust these settings:**

- Use higher `vector_weight` (0.7-0.9) when semantic similarity is more
  important than keyword matching
- Use lower `vector_weight` (0.3-0.5) when exact keyword matches are important
- Disable hybrid search when using views without an `id_column` configured,
  or when BM25 overhead is not acceptable


