# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo \
    -ldflags="-s -w" \
    -o pgedge-rag-server ./cmd/pgedge-rag-server

# Runtime stage
FROM registry.access.redhat.com/ubi9/ubi-micro:latest

# Create a pgedge user
RUN echo "pgedge:x:1000:1000:pgedge:/app:/sbin/nologin" >> /etc/passwd && \
    echo "pgedge:x:1000:" >> /etc/group

# Set working directory
WORKDIR /app

# Copy CA certificates from builder (alpine has them installed)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy binary from builder
COPY --from=builder /build/pgedge-rag-server .

# Create directory for config files
RUN mkdir -p /etc/pgedge && \
    chown -R pgedge:pgedge /app /etc/pgedge

# Switch to pgedge user
USER pgedge

# Expose default port
EXPOSE 8080

# Note: Health check removed as curl/wget are not available in ubi-micro
# Users can add health checks at the orchestration layer (docker-compose, kubernetes, etc.)

# Run the binary
ENTRYPOINT ["/app/pgedge-rag-server"]
CMD ["-config", "/etc/pgedge/pgedge-rag-server.yaml"]
