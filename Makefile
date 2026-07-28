.PHONY: build test lint fmt all clean openapi docs vulncheck

# Build the binary
build:
	go build -o bin/pgedge-rag-server ./cmd/pgedge-rag-server

# Generate static OpenAPI specification for documentation
openapi: build
	./bin/pgedge-rag-server -openapi > docs/openapi.json

# Build documentation (includes OpenAPI spec generation)
docs: openapi

# Run all tests with race detection and coverage
test:
	go test -v -race -coverprofile=coverage.out ./...

# Run the linter
lint:
	golangci-lint run ./...

# Check formatting (fails if files need formatting)
fmt:
	gofmt -w -s .
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "The following files need formatting:"; \
		gofmt -l .; \
		exit 1; \
	fi

# Check dependencies and the standard library for known vulnerabilities.
#
# This uses call-graph analysis, so it reports whether a vulnerable
# symbol is actually reachable from our code. A container image scan
# may or may not detect a vulnerable Go module version at all,
# depending on whether it inspects binary buildinfo; none can tell you
# whether the vulnerable code path is reachable, which is what this
# adds.
vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

# Run all checks: format, lint, test, and build
all: fmt lint test build

# Clean build artifacts
clean:
	rm -rf bin/ coverage.out
