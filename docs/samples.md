# Sample Configurations

The examples on this page demonstrate using [configuration options](configuration.md) in a file to specify behavior preferences for the RAG server.

## Minimal Configuration

```yaml
pipelines:
  - name: "docs"
    database:
      host: "localhost"
      database: "mydb"
    tables:
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

## Production Configuration with TLS

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
    tables:
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

## Local Development with Ollama

```yaml
pipelines:
  - name: "local-docs"
    description: "Local document search"
    database:
      host: "localhost"
      database: "devdb"
    tables:
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

## Using Defaults for Multiple Pipelines

This configuration uses defaults to avoid repeating LLM settings across
multiple pipelines. Individual pipelines can override specific settings:

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

pipelines:
  # This pipeline uses all defaults
  - name: "docs"
    description: "Documentation search"
    database:
      host: "localhost"
      database: "docs_db"
    tables:
      - table: "documents"
        text_column: "content"
        vector_column: "embedding"

  # This pipeline overrides the completion model
  - name: "support"
    description: "Support knowledge base"
    database:
      host: "localhost"
      database: "support_db"
    tables:
      - table: "tickets"
        text_column: "resolution"
        vector_column: "embedding"
    rag_llm:
      provider: "anthropic"
      model: "claude-haiku-3-5-20241022"
    token_budget: 2000

  # This pipeline uses a different embedding provider
  - name: "research"
    description: "Research papers"
    database:
      host: "localhost"
      database: "research_db"
    tables:
      - table: "papers"
        text_column: "abstract"
        vector_column: "embedding"
    embedding_llm:
      provider: "voyage"
      model: "voyage-3"
```

## Using an API Gateway

This configuration routes LLM requests through an API gateway
(e.g. [Portkey](https://portkey.ai)) using custom `base_url`
values. The `base_url` can be set in defaults to apply to all
pipelines, or per-pipeline to override the default:

```yaml
defaults:
  embedding_llm:
    provider: "openai"
    model: "text-embedding-3-small"
    base_url: "https://gateway.example.com/v1"
  rag_llm:
    provider: "anthropic"
    model: "claude-sonnet-4-20250514"
    base_url: "https://gateway.example.com/anthropic"

pipelines:
  - name: "via-gateway"
    description: "Pipeline routed through API gateway"
    database:
      host: "localhost"
      database: "mydb"
    tables:
      - table: "documents"
        text_column: "content"
        vector_column: "embedding"
```

## Voyage Embeddings with Anthropic Completion

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
    tables:
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
