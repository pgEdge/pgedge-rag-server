# Installation

The pgEdge RAG Server can be installed by building from source or with
pgEdge Enterprise Postgres packages. For details about installing the
RAG server with a package, see the platform-specific documentation for
[pgEdge Enterprise Postgres](https://docs.pgedge.com/enterprise/).

Before installing the pgEdge RAG Server, you should install:

- Go, version 1.23 or later.
- PostgreSQL 14 or later.
- [pgvector](https://github.com/pgvector/pgvector).

You will also need [API keys](keys.md) for your chosen LLM providers.

## Building from Source

Before building the binary, clone the RAG server repository and navigate into
the root of the repo:

```bash
git clone https://github.com/pgedge/pgedge-rag-server.git
cd pgedge-rag-server
```

Build the pgEdge RAG server binary with the command; the binary is created in
the `bin` directory:

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
