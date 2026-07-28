# Developer Notes

**Prerequisites**

- Go 1.26.5 or later
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

Use the following command to check the dependencies and the Go standard
library for known vulnerabilities:

```bash
make vulncheck
```

This runs `govulncheck`, which uses call-graph analysis to report only
those vulnerabilities whose affected symbols are genuinely reachable
from this codebase. It is worth running in addition to any container
image scanning, rather than instead of it, because the two look at
different things: an image scan may or may not detect a vulnerable Go
module version, depending on whether it inspects binary buildinfo, but
none can tell you whether the vulnerable code path is reachable.

## Support

- [GitHub Issues](https://github.com/pgEdge/pgedge-rag-server/issues)
- Full documentation is available at [the pgEdge website](https://docs.pgedge.com/pgedge-rag-server/).


## License

This project is licensed under the [PostgreSQL License](LICENCE.md).