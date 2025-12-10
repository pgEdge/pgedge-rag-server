# pgEdge RAG Server Tutorial

Before installing the RAG server, install or gather the following:

- Go 1.24 or later
- PostgreSQL, installed with the [`pgvector`](https://github.com/pgvector/pgvector "`pgvector`") extension created
- API keys for your chosen LLM providers

**Installing pgEdge RAG Server**

Use the following commands to clone the pgedge-rag-server repository and build
RAG Server:

```bash
# Clone the repository
git clone https://github.com/pgEdge/pgedge-rag-server.git
cd pgedge-rag-server

# Build the binary
make build
```

**Creating a Configuration File**

pgEdge RAG Server looks for a configuration file in:

1. `/etc/pgedge/pgedge-rag-server.yaml`
2. `pgedge-rag-server.yaml` (in the binary's directory)

When you invoke [`pgedge-rag-server`](usage.md) you can use the `--config` option
to specify the complete path to a custom location for the configuration file.

Create a `config.yaml` file:

```yaml
pipelines:
  - name: "my-docs"
    description: "Search my documentation"
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

Set your API key:

```bash
export OPENAI_API_KEY="sk-..."
```

**Run the server**


./bin/pgedge-rag-server -config config.yaml


### Query the Server

```bash
# List available pipelines
curl http://localhost:8080/v1/pipelines

# Ask a question
curl -X POST http://localhost:8080/v1/pipelines/my-docs \
  -H "Content-Type: application/json" \
  -d '{"query": "How do I get started?"}'
```