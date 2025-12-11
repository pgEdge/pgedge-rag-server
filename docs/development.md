# Developer Notes

**Prerequisites**

- Go 1.24 or later
- PostgreSQL (for integration tests)
- Python 3.12+ (for documentation)

Use the following command to run all of the checks and build `pgedge-rag-server`:

```bash
# Run all checks (format, lint, test, build)
make all
```

Use the following command to run the RAG server test suite:

```bash
make test
```

Use the following command to run the Go Linter:

```bash
make lint
```

Use the following command to streamline and format the code:

```bash
make fmt
```

## Support

- [GitHub Issues](https://github.com/pgEdge/pgedge-rag-server/issues)
- Full documentation is available at [the pgEdge website](https://docs.pgedge.com/pgedge-rag-server/).


## License

This project is licensed under the [PostgreSQL License](LICENCE.md).