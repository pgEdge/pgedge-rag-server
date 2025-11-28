.PHONY: build test lint fmt all clean openapi docs

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

# Run all checks: format, lint, test, and build
all: fmt lint test build

# Clean build artifacts
clean:
	rm -rf bin/ coverage.out
