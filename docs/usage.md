# Using pgEdge RAG Server

After building the binary and creating a configuration file, you can invoke `pgedge-rag-server`.  Use the command:

```bash
./bin/pgedge-rag-server (options)
```

You can include the following options when invoking the server:

| Option     | Description                               |
|------------|-------------------------------------------|
| `-config`  | Path to configuration file (see below)    |
| `-openapi` | Output OpenAPI v3 specification and exit  |
| `-version` | Show version information and exit         |
| `-help`    | Show help message and exit                |

When you invoke `pgedge-rag-server` you can optionally include the `--config` option to specify the complete path to a custom location for the configuration file.  If you do not specify a location on the command line, the server searches for configuration files in:

1. `/etc/pgedge/pgedge-rag-server.yaml`
2. `pgedge-rag-server.yaml` (in the binary's directory)