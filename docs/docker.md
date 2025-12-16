# Docker Deployment

This guide explains how to deploy pgEdge RAG Server using Docker and
Docker Compose.

## Prerequisites

Before deploying with Docker, ensure you have:

- Docker Engine 20.10 or later
- Docker Compose V2 or later
- API keys for your chosen LLM providers
  (see [Managing API Keys](keys.md))

## Quick Start

### 1. Clone the Repository

```bash
git clone https://github.com/pgedge/pgedge-rag-server.git
cd pgedge-rag-server
```

### 2. Configure Environment Variables

Copy the example environment file and configure your API keys:

```bash
cp docker.env.example .env
```

Edit the `.env` file and add your API keys:

```bash
# Required: Add your API keys
OPENAI_API_KEY=sk-your-openai-key-here
ANTHROPIC_API_KEY=sk-ant-your-anthropic-key-here

# Optional: Customize ports and credentials
POSTGRES_PASSWORD=your-secure-password
RAG_SERVER_PORT=8080
```

### 3. Configure the RAG Server

The repository includes a sample configuration file
`pgedge-rag-server.yaml`. Review and customize it for your needs:

- Update database credentials if you changed them in `.env`
- Configure your pipelines with appropriate tables and columns
- Select your preferred embedding and LLM models

See [Creating a Configuration File](configuration.md) for detailed
configuration options.

### 4. Start the Services

```bash
docker compose up -d
```

This command will:

- Pull the PostgreSQL with pgvector image
- Build the RAG server Docker image
- Start both services
- Initialize the database with pgvector extension and sample schema

### 5. Verify the Deployment

Check that the services are running:

```bash
docker compose ps
```

Test the RAG server:

```bash
curl http://localhost:8080/v1/pipelines
```

## Using Pre-built Images

Instead of building locally, you can use pre-built images from GitHub
Container Registry:

```bash
docker pull ghcr.io/pgedge/rag-server:latest
```

Update your `docker-compose.yml` to use the pre-built image:

```yaml
services:
    rag-server:
        image: ghcr.io/pgedge/rag-server:latest
        # Remove the 'build' section
```

## Database Initialization

The included `init-db.sql` script automatically:

- Enables the pgvector extension
- Creates a sample `documents` table with vector columns
- Creates indexes for vector similarity search
- Creates indexes for BM25 text search (hybrid mode)

### Customizing the Schema

To customize the database schema:

1. Edit `init-db.sql` to match your data structure
2. Adjust vector dimensions based on your embedding model:
   - OpenAI text-embedding-3-small: 1536 dimensions
   - OpenAI text-embedding-3-large: 3072 dimensions
   - Voyage AI models: 1024 or 1536 dimensions
3. Update `pgedge-rag-server.yaml` to reference your table and column
   names
4. Restart the services: `docker compose down && docker compose up -d`

## Populating Your Database

After starting the services, populate your database with content:

```bash
# Connect to the PostgreSQL container
docker compose exec postgres psql -U postgres -d ragdb

# Insert sample documents (adjust vector dimensions as needed)
INSERT INTO documents (content, title, source) VALUES
('Your document content here', 'Document Title', 'source-name');
```

For production use, you'll typically:

1. Generate embeddings using your embedding model
2. Insert both the text content and embeddings into the database
3. Ensure the vector dimensions match your embedding model

## Managing the Deployment

### View Logs

```bash
# All services
docker compose logs -f

# Specific service
docker compose logs -f rag-server
docker compose logs -f postgres
```

### Stop Services

```bash
docker compose stop
```

### Restart Services

```bash
docker compose restart
```

### Remove Services and Data

```bash
# Stop and remove containers (preserves data volumes)
docker compose down

# Remove everything including data volumes
docker compose down -v
```

### Update the RAG Server

To update to a new version:

```bash
# Pull the latest image
docker compose pull rag-server

# Restart the service
docker compose up -d rag-server
```

## Production Considerations

For production deployments, consider:

### Security

- **Never commit `.env` files** to version control
- Use strong passwords for PostgreSQL
- Enable TLS/HTTPS for the RAG server (see
  [Configuration](configuration.md))
- Restrict network access using Docker network policies
- Use secrets management (Docker Secrets, Kubernetes Secrets, etc.)

### Data Persistence

The docker-compose setup uses Docker volumes for PostgreSQL data:

```yaml
volumes:
    postgres_data:
        driver: local
```

For production:

- Use named volumes or bind mounts to specific host paths
- Implement regular backup strategies
- Consider using managed PostgreSQL services

### Resource Limits

Add resource constraints to your `docker-compose.yml`:

```yaml
services:
    rag-server:
        deploy:
            resources:
                limits:
                    cpus: '2'
                    memory: 2G
                reservations:
                    cpus: '1'
                    memory: 1G
```

### High Availability

For high availability:

- Deploy multiple RAG server instances behind a load balancer
- Use PostgreSQL replication for database redundancy
- Consider orchestration platforms like Kubernetes

## Troubleshooting

### Service Won't Start

Check logs for errors:

```bash
docker compose logs rag-server
```

Common issues:

- Missing or invalid API keys in `.env`
- Configuration file syntax errors
- Port conflicts (8080 or 5432 already in use)

### Cannot Connect to PostgreSQL

Verify the database is ready:

```bash
docker compose exec postgres pg_isready -U postgres
```

If the database isn't ready, wait a few moments for initialization to
complete.

### pgvector Extension Not Found

Ensure you're using the `pgvector/pgvector` Docker image, which
includes the extension pre-installed.

### Configuration Changes Not Applied

After modifying `pgedge-rag-server.yaml`:

```bash
docker compose restart rag-server
```

The configuration file is mounted read-only, so changes on the host are
immediately available after restart.

## Additional Resources

- [API Reference](api/reference.md)
- [Configuration Guide](configuration.md)
- [Managing API Keys](keys.md)
- [Docker Documentation](https://docs.docker.com/)
- [Docker Compose Documentation](https://docs.docker.com/compose/)
