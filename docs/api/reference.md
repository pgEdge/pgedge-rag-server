# API Reference

The pgEdge RAG Server provides a REST API for querying RAG pipelines.

## Base URL

By default, the server listens on `http://localhost:8080`. All endpoints use
the `/v1` API version prefix.

## API Discovery

The server implements [RFC 8631](https://www.rfc-editor.org/rfc/rfc8631.html)
for API documentation discovery. All JSON responses include a `Link` header:

```
Link: </v1/openapi.json>; rel="service-desc"
```

This allows tools like [restish](https://rest.sh/) to automatically discover
and use the API schema.

## Endpoints

### OpenAPI Specification

Get the OpenAPI v3 specification for the API.

```
GET /v1/openapi.json
```

#### Response

Returns an OpenAPI 3.0.3 specification document describing all API endpoints,
request/response schemas, and error formats.

| Status Code | Description              |
|-------------|--------------------------|
| 200         | OpenAPI specification    |

---

### Health Check

Check if the server is running and healthy.

```
GET /v1/health
```

#### Response

```json
{
  "status": "healthy"
}
```

| Status Code | Description        |
|-------------|--------------------|
| 200         | Server is healthy  |

---

### List Pipelines

Get a list of all available RAG pipelines.

```
GET /v1/pipelines
```

#### Response

```json
{
  "pipelines": [
    {
      "name": "my-docs",
      "description": "Search my documentation"
    },
    {
      "name": "knowledge-base",
      "description": "Corporate knowledge base"
    }
  ]
}
```

| Status Code | Description              |
|-------------|--------------------------|
| 200         | List of pipelines        |

---

### Query Pipeline

Execute a RAG query against a specific pipeline.

```
POST /v1/pipelines/{name}
```

#### Path Parameters

| Parameter | Description                    |
|-----------|--------------------------------|
| `name`    | Pipeline name (from config)    |

#### Request Body

```json
{
  "query": "How do I configure replication?",
  "stream": false,
  "top_n": 10,
  "filter": "product = 'pgEdge' AND version = 'v5.0'",
  "include_sources": true,
  "messages": [
    {"role": "user", "content": "What is pgEdge?"},
    {"role": "assistant", "content": "pgEdge is a distributed PostgreSQL platform..."}
  ]
}
```

| Field             | Type    | Required | Description                               |
|-------------------|---------|----------|-------------------------------------------|
| `query`           | string  | Yes      | The question to answer                    |
| `stream`          | boolean | No       | Enable streaming response (SSE)           |
| `top_n`           | integer | No       | Override default result limit             |
| `filter`          | string  | No       | SQL WHERE clause to filter results        |
| `include_sources` | boolean | No       | Include source documents (default: false) |
| `messages`        | array   | No       | Previous conversation history for context |

The `filter` parameter allows you to pass a SQL WHERE clause fragment to
filter search results. This is useful when your data contains multiple
products or versions and you want to restrict results. For example:

- `"product = 'pgAdmin'"` - Filter by product
- `"version = 'v9.0'"` - Filter by version
- `"product = 'pgAdmin' AND version >= 'v8.0'"` - Combined filters

!!! warning "Security Note"

    The filter is passed directly to the database. Ensure your application
    validates filter values if they come from untrusted sources.

##### Message Object

| Field     | Type   | Description                              |
|-----------|--------|------------------------------------------|
| `role`    | string | Message role: `user` or `assistant`      |
| `content` | string | Message content                          |

#### Non-Streaming Response

```json
{
  "answer": "To configure replication, you need to...",
  "tokens_used": 1523
}
```

When `include_sources: true`:

```json
{
  "answer": "To configure replication, you need to...",
  "sources": [
    {
      "id": "doc-123",
      "content": "Replication is configured by...",
      "score": 0.95
    },
    {
      "id": "doc-456",
      "content": "The replication settings include...",
      "score": 0.87
    }
  ],
  "tokens_used": 1523
}
```

| Field        | Type   | Description                              |
|--------------|--------|------------------------------------------|
| `answer`     | string | The generated answer                     |
| `sources`    | array  | Source documents (only if requested)     |
| `tokens_used`| integer| Total tokens consumed by the request     |

##### Source Object

| Field     | Type   | Description                           |
|-----------|--------|---------------------------------------|
| `id`      | string | Document identifier (if available)    |
| `content` | string | Document text content                 |
| `score`   | number | Relevance score (higher is better)    |

#### Streaming Response

When `stream: true`, the response uses Server-Sent Events (SSE).

**Headers:**

```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
```

**Event Format:**

Each event is a JSON object sent as an SSE data line:

```
data: {"type": "chunk", "content": "To configure "}

data: {"type": "chunk", "content": "replication, "}

data: {"type": "chunk", "content": "you need to..."}

data: {"type": "done"}
```

##### Event Types

| Type    | Description                         | Fields                |
|---------|-------------------------------------|-----------------------|
| `chunk` | Partial response content            | `content`             |
| `done`  | Stream completed successfully       | -                     |
| `error` | An error occurred                   | `error`               |

#### Error Responses

```json
{
  "error": {
    "code": "PIPELINE_NOT_FOUND",
    "message": "pipeline not found: unknown-pipeline"
  }
}
```

| Status Code | Error Code           | Description                    |
|-------------|----------------------|--------------------------------|
| 400         | `INVALID_REQUEST`    | Invalid request body or query  |
| 404         | `PIPELINE_NOT_FOUND` | Pipeline does not exist        |
| 405         | `METHOD_NOT_ALLOWED` | Wrong HTTP method              |
| 500         | `EXECUTION_ERROR`    | Pipeline execution failed      |
| 500         | `INTERNAL_ERROR`     | Unexpected server error        |

---

## Examples

### cURL

**List pipelines:**

```bash
curl http://localhost:8080/v1/pipelines
```

**Simple query:**

```bash
curl -X POST http://localhost:8080/v1/pipelines/my-docs \
  -H "Content-Type: application/json" \
  -d '{"query": "How do I get started?"}'
```

**Query with filter:**

```bash
curl -X POST http://localhost:8080/v1/pipelines/my-docs \
  -H "Content-Type: application/json" \
  -d '{"query": "How do I configure backups?", "filter": "product = '\''pgAdmin'\'' AND version = '\''v9.0'\''"}'
```

**Streaming query:**

```bash
curl -X POST http://localhost:8080/v1/pipelines/my-docs \
  -H "Content-Type: application/json" \
  -N \
  -d '{"query": "Explain the architecture", "stream": true}'
```

### Python

**Non-streaming:**

```python
import requests

response = requests.post(
    "http://localhost:8080/v1/pipelines/my-docs",
    json={"query": "How do I configure SSL?"}
)

data = response.json()
print(data["answer"])

for source in data["sources"]:
    print(f"- {source['content'][:100]}... (score: {source['score']:.2f})")
```

**Streaming:**

```python
import requests

response = requests.post(
    "http://localhost:8080/v1/pipelines/my-docs",
    json={"query": "Explain the setup process", "stream": True},
    stream=True
)

for line in response.iter_lines():
    if line and line.startswith(b"data: "):
        import json
        event = json.loads(line[6:])
        if event["type"] == "chunk":
            print(event["content"], end="", flush=True)
        elif event["type"] == "done":
            print()  # newline at end
```

### JavaScript

**Non-streaming:**

```javascript
const response = await fetch("http://localhost:8080/v1/pipelines/my-docs", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ query: "How do I get started?" }),
});

const data = await response.json();
console.log(data.answer);
```

**Streaming with EventSource:**

```javascript
// Using fetch for SSE
const response = await fetch("http://localhost:8080/v1/pipelines/my-docs", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ query: "Explain the setup", stream: true }),
});

const reader = response.body.getReader();
const decoder = new TextDecoder();

while (true) {
  const { done, value } = await reader.read();
  if (done) break;

  const text = decoder.decode(value);
  const lines = text.split("\n");

  for (const line of lines) {
    if (line.startsWith("data: ")) {
      const event = JSON.parse(line.slice(6));
      if (event.type === "chunk") {
        process.stdout.write(event.content);
      }
    }
  }
}
```

## Rate Limiting

The server does not implement rate limiting. If needed, use a reverse proxy
(nginx, Caddy, etc.) or API gateway in front of the server.

## Authentication

The server does not implement authentication. For production deployments,
place the server behind an authenticating proxy or API gateway.
