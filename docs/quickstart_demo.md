# pgEdge AI Toolkit RAG Server Quickstart

This quickstart guide demonstrates how to get started with the RAG Server.
The RAG Server demo includes the following features:

- A PostgreSQL database preseeded with documentation content.
- A pgEdge RAG Server with two configured pipelines for documentation
  search.
- A web client that provides a simple interface for testing RAG
  pipelines.

## Prerequisites

This section describes the required software and credentials needed to
run the demo.

- Install Docker Desktop by following the instructions at https://docs.docker.com/get-docker/.
- Obtain an OpenAI API key by following the guidance at https://platform.openai.com/docs.

After meeting the prerequisites, you can install the RAG Server demo
using one of the following options:

- A [One-Step Quickstart](#one-step-quickstart) that uses default
  settings to launch the demo.
- A [Three-Step Quickstart](#three-step-quickstart) that allows you to
  configure settings before launching the demo.

## One-Step Quickstart

The single command option is the fastest way to get started. Execute
the following command:

```bash
/bin/sh -c "$(curl -fsSL https://downloads.pgedge.com/quickstart/rag/pgedge-rag-demo.sh)"
```

This command performs the following actions:

- Downloads *docker-compose.yml* and *.env.example* from the same
  location.
- Prompts you for your API key(s) securely.
- Starts all services automatically.
- Displays connection details when ready.

!!! note

    The installer creates a temporary workspace in */var* and runs the
    demo from that location.

Sample output from running the demo script:

```bash
========================================
pgEdge RAG Server - Quickstart Demo
========================================

ℹ  Checking dependencies...
✓  All dependencies found

ℹ  This demo requires an OpenAI API key for embeddings
ℹ  Anthropic API key is optional (for Claude completions)

Enter your OpenAI API key
(input is hidden, paste is OK):
Enter your Anthropic API key (optional, press Enter to skip)
(input is hidden, paste is OK):

ℹ  Creating .env configuration file...
✓  .env file created

ℹ  Starting services with Docker Compose...
[+] Running 5/5
 ✔ Network pgedge-rag-demo_pgedge-rag-network  Created                                                                                                                                                       0.0s
 ✔ Volume pgedge-rag-demo_postgres-data        Created                                                                                                                                                       0.0s
 ✔ Container ragdb                             Healthy                                                                                                                                                       5.7s
 ✔ Container rag-server                        Started                                                                                                                                                       5.7s
 ✔ Container web-client                        Started                                                                                                                                                       5.8s
✓  Services started successfully

ℹ  Waiting for ragdb to become healthy...
✓  ragdb is ready

========================================
pgEdge RAG Server Demo is Running!
========================================

Web Client:
  http://localhost:3001
  Interactive UI for testing RAG pipelines

RAG Server API:
  URL: http://localhost:8081

Available Pipelines:
  1. postgresql-docs - PostgreSQL product documentation
     POST http://localhost:8081/v1/pipelines/postgresql-docs

  2. pgedge-docs - pgEdge Platform documentation
     POST http://localhost:8081/v1/pipelines/pgedge-docs

Example Query:
  curl -X POST http://localhost:8081/v1/pipelines/postgresql-docs \
    -H "Content-Type: application/json" \
    -d '{"query": "How do I create a table?"}'

Database Connection:
  Host: localhost:5433
  Database: ragdb
  User: docuser
  Password: docpass

  Connect: docker exec -it ragdb psql -U docuser -d ragdb

========================================

Workspace: /var/folders/t6/s1v3jgsj5vn8gn5s6zjbz8d00000gn/T/tmp.jc6BD4Eimz

To stop: cd /var/folders/t6/s1v3jgsj5vn8gn5s6zjbz8d00000gn/T/tmp.jc6BD4Eimz && docker compose down
To restart: cd /var/folders/t6/s1v3jgsj5vn8gn5s6zjbz8d00000gn/T/tmp.jc6BD4Eimz && docker compose restart
To view logs: cd /var/folders/t6/s1v3jgsj5vn8gn5s6zjbz8d00000gn/T/tmp.jc6BD4Eimz && docker compose logs -f

Documentation: https://docs.pgedge.com/pgedge-rag-server
========================================
```

Navigate to the URL for the RAG Server web client at
http://localhost:3001. The web client provides an overview of the demo
and sample queries that can be submitted to the RAG server.

## Three-Step Quickstart

For a more traditional setup, follow these steps.

### Step 1: Create a Working Directory

```bash
mkdir ~/pgedge-rag-demo
cd ~/pgedge-rag-demo
```

### Step 2: Download the Demo Artifacts

```bash
curl -fsSLO https://downloads.pgedge.com/quickstart/rag/docker-compose.yml
curl -fsSLO https://downloads.pgedge.com/quickstart/rag/.env.example
```

### Step 3: Configure Your API Key

```bash
cp .env.example .env
```

Edit *.env* and add `OPENAI_API_KEY` and optionally
`ANTHROPIC_API_KEY`.

Start the Docker container with the following command:

```bash
docker compose up -d
```

During deployment, the following steps occur:

1. PostgreSQL starts and downloads the documentation sets
   (approximately 35 MB).
2. The RAG server starts and configures pipelines for the PostgreSQL
   and pgEdge documentation sets.
3. A simple web UI becomes available that allows you to submit sample
   queries.

Once all services are healthy (approximately 60 seconds), you can
access them as follows:

```bash
# Web Client Interface
http://localhost:3001

# PostgreSQL Database
Host: localhost:5433
Database: ragdb
User: docuser / docpass
Connect: psql -h localhost -p 5433 -U docuser -d ragdb
Or: docker exec -it ragdb psql -U docuser -d ragdb

# RAG Server API
http://localhost:8081

# Example 1: List available pipelines
curl -s http://localhost:8081/v1/pipelines | jq

# Example 2: Query the PostgreSQL documentation
curl -X POST http://localhost:8081/v1/pipelines/postgresql-docs \
  -H "Content-Type: application/json" \
  -d '{"query": "How do I create a table?"}'
```

Navigate to the URL for the RAG Server web client at
http://localhost:3001. The web client provides an overview of the demo
and sample queries that can be submitted to the RAG server.

## Managing the Service and Reviewing Log Files

Use the following commands to stop the server.

Stop the services while retaining data:

```bash
docker compose down
```

Stop the services and remove volumes to create a fresh start:

```bash
docker compose down -v
```

Use the following command to view the log files for all services:

```bash
docker compose logs -f
```

Review the log file for a specific service:

```bash
docker compose logs -f rag-server
docker compose logs -f ragdb
docker compose logs -f web-client
```
