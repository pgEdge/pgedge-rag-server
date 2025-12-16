# Docker Deployment Quick Reference

This document provides a quick reference for deploying pgEdge RAG
Server using Docker.

## Quick Start

```bash
# 1. Clone the repository
git clone https://github.com/pgedge/pgedge-rag-server.git
cd pgedge-rag-server

# 2. Configure environment variables
cp docker.env.example .env
# Edit .env and add your API keys

# 3. Start the services
docker compose up -d

# 4. Verify deployment
curl http://localhost:8080/v1/pipelines
```

## Files Overview

- **Dockerfile**: Multi-stage build configuration for the RAG server
- **docker-compose.yml**: Complete stack with PostgreSQL and RAG server
- **pgedge-rag-server.yaml**: Configuration file for the RAG server
- **docker.env.example**: Template for environment variables
- **init-db.sql**: Database initialization script with pgvector setup

## Pre-built Images

Use the pre-built image from GitHub Container Registry:

```bash
docker pull ghcr.io/pgedge/rag-server:latest
```

## Common Commands

```bash
# View logs
docker compose logs -f rag-server

# Restart services
docker compose restart

# Stop services
docker compose stop

# Remove everything
docker compose down -v

# Rebuild after changes
docker compose up -d --build
```

## Configuration

The RAG server expects configuration at
`/etc/pgedge/pgedge-rag-server.yaml` inside the container. The
docker-compose setup mounts the local `pgedge-rag-server.yaml` file to
this location.

API keys are passed via environment variables:

- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `VOYAGE_API_KEY`

## GitHub Actions Workflow

The repository includes a GitHub Actions workflow (`.github/workflows/docker.yml`)
that automatically:

- Builds the Docker image on push to main or tags
- Pushes images to GitHub Container Registry
- Tags images appropriately (latest, version numbers)
- Supports multi-platform builds (amd64, arm64)

### Triggering a Release

To trigger a Docker image release:

```bash
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

This will build and push the image with tags:

- `ghcr.io/pgedge/rag-server:latest`
- `ghcr.io/pgedge/rag-server:v1.0.0`
- `ghcr.io/pgedge/rag-server:1.0`
- `ghcr.io/pgedge/rag-server:1`

## Database Setup

The `init-db.sql` script automatically:

- Enables pgvector extension
- Creates a sample documents table with vector columns
- Creates indexes for vector and text search

Customize this script for your specific schema needs.

## Documentation

Full documentation available at: [docs/docker.md](docs/docker.md)

For more information, see:

- [Configuration Guide](docs/configuration.md)
- [API Reference](docs/api/reference.md)
- [Managing API Keys](docs/keys.md)
