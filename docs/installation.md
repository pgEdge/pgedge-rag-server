# Installation

Before installing pgEdge RAG Server, you should install or obtain:

- Go 1.24 or later
- PostgreSQL 14 or later, with [pgvector installed](https://github.com/pgvector/pgvector)
- [API keys](keys.md) for your chosen LLM providers

## Building from Source

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
