# Installation

## Prerequisites

- Go 1.24 or later
- PostgreSQL 14 or later, with [pgvector installed]()
- API keys for your chosen LLM providers

## Building from Source

### Clone the Repository

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
