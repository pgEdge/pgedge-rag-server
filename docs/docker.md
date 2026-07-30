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
- **Put an authenticating proxy in front of the server**; see the
  warning below
- Restrict network access using Docker network policies
- Use secrets management (Docker Secrets, Kubernetes Secrets, etc.)

!!! warning "TLS is not access control"

    The RAG server implements neither client authentication nor rate
    limiting, so every endpoint it exposes is reachable by anyone who
    can reach the port, and any such caller can issue as many queries
    as they like. Enabling TLS encrypts the connection and authenticates
    the *server* to the client, but it does nothing to establish who
    the client is, and it places no bound on how often they may call;
    a TLS-only deployment is still an open one. A production setup
    therefore needs an authenticating reverse proxy or API gateway
    (nginx, Caddy, Envoy, or similar) in front of the service, handling
    both authentication and rate limiting, with the RAG server itself
    bound to a private network rather than published directly. See
    [Authentication](api/reference.md#authentication) and
    [Rate Limiting](api/reference.md#rate-limiting) in the API
    reference.

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

The server normally picks up configuration changes on its own, without a
restart, as described under
[Configuration Reloading](configuration.md#configuration-reloading).
The `docker-compose.yml` shipped here is the exception, because it
bind-mounts the configuration as a single file:

```yaml
volumes:
    - ./pgedge-rag-server.yaml:/etc/pgedge/pgedge-rag-server.yaml:ro
```

Change detection works by watching the directory that contains the
watched file rather than the file itself, and with a single-file bind
mount the directory the container sees (`/etc/pgedge`) is not the host
directory you edited in, so an edit on the host generates no event
inside the container and no reload follows. Editors that save by writing
a temporary file and renaming it over the original are worse still: the
rename replaces the host inode, which detaches the bind mount
altogether, leaving the container reading the original file
indefinitely.

So under Docker, after modifying `pgedge-rag-server.yaml`, restart the
service:

```bash
docker compose restart rag-server
```

The mount being read-only does not get in the way of this; it only stops
the *server* writing to the file.

If you would rather have automatic reloads in a container, mount the
containing directory instead of the individual file, so that the
container and the host share the directory the watcher is watching:

```yaml
volumes:
    - ./config:/etc/pgedge:ro
```

with `pgedge-rag-server.yaml` inside `./config`. This is also how a
Kubernetes `ConfigMap` volume behaves, which is why reloads work there
without any of this ceremony. Note that changes to server-level
settings (`listen_address`, `port`, `tls`, and `cors`) are read only at
startup and always need a restart, however the file is mounted.

## Additional Resources

- [API Reference](api/reference.md)
- [Configuration Guide](configuration.md)
- [Managing API Keys](keys.md)
- [Docker Documentation](https://docs.docker.com/)
- [Docker Compose Documentation](https://docs.docker.com/compose/)
