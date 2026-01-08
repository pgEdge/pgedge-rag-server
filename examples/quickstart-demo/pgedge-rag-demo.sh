#!/bin/sh
set -eu

# ==============================================================================
# Configuration
# ==============================================================================

BASE_URL="https://downloads.pgedge.com/quickstart/rag"
FILES="docker-compose.yml .env.example"

# Workspace directory (created at runtime)
WORKDIR=""

# Initialize variables (script runs with `set -u`)
OPENAI_KEY=""
ANTHROPIC_KEY=""
DOCKER_COMPOSE=""

# ==============================================================================
# Terminal Formatting Functions
# ==============================================================================

# Detect color support
if command -v tput >/dev/null 2>&1 && tput setaf 1 >/dev/null 2>&1; then
    BOLD=$(tput bold)
    DIM=$(tput dim)
    RESET=$(tput sgr0)
    RED=$(tput setaf 1)
    GREEN=$(tput setaf 2)
    YELLOW=$(tput setaf 3)
    CYAN=$(tput setaf 6)
else
    BOLD=""
    DIM=""
    RESET=""
    RED=""
    GREEN=""
    YELLOW=""
    CYAN=""
fi

# Output functions
info() {
    printf "${CYAN}ℹ${RESET}  %s\n" "$1"
}

ok() {
    printf "${GREEN}✓${RESET}  %s\n" "$1"
}

warn() {
    printf "${YELLOW}!${RESET}  %s\n" "$1"
}

err() {
    printf "${RED}✗${RESET}  %s\n" "$1" >&2
}

# ==============================================================================
# Download Helper
# ==============================================================================

download() {
    url="$1"
    out="$2"

    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$url" -o "$out"
    elif command -v wget >/dev/null 2>&1; then
        wget -q "$url" -O "$out"
    else
        err "Need curl or wget to download files"
        exit 1
    fi
}

# ==============================================================================
# Dependency Checking
# ==============================================================================

check_dependencies() {
    info "Checking dependencies..."

    # Check for docker
    if ! command -v docker >/dev/null 2>&1; then
        err "Docker is not installed"
        err "Please install Docker from: https://docs.docker.com/get-docker/"
        exit 1
    fi

    # Check for docker compose (modern) or docker-compose (legacy)
    if docker compose version >/dev/null 2>&1; then
        DOCKER_COMPOSE="docker compose"
    elif command -v docker-compose >/dev/null 2>&1; then
        DOCKER_COMPOSE="docker-compose"
    else
        err "Docker Compose is not available"
        err "Please install Docker Compose"
        exit 1
    fi

    ok "All dependencies found"
}

# ==============================================================================
# API Key Validation
# ==============================================================================

validate_api_key() {
    key="$1"

    # Allow empty keys (for optional keys)
    if [ -z "$key" ]; then
        return 0
    fi

    # Check minimum length
    if [ ${#key} -lt 20 ]; then
        err "API key is too short (minimum 20 characters)"
        return 1
    fi

    # Check for valid characters (alphanumeric, dots, hyphens, underscores)
    if ! echo "$key" | grep -q '^[a-zA-Z0-9._-]*$'; then
        err "API key contains invalid characters"
        err "Only alphanumeric, dots, hyphens, and underscores are allowed"
        return 1
    fi

    return 0
}

# ==============================================================================
# User Input Functions
# ==============================================================================

prompt_secret() {
    prompt="$1"
    var_name="$2"
    optional="${3:-false}"

    printf "${BOLD}%s${RESET}" "$prompt"
    if [ "$optional" = "true" ]; then
        printf " ${DIM}(optional, press Enter to skip)${RESET}"
    fi
    printf "\n"
    printf "${DIM}(input is hidden, paste is OK)${RESET}: "

    # Disable bracketed paste mode and echo to ensure reliable input
    printf '\e[?2004l' >/dev/tty 2>/dev/null || true
    stty -echo 2>/dev/null || true

    read -r value

    # Re-enable echo and bracketed paste mode
    stty echo 2>/dev/null || true
    printf '\e[?2004h' >/dev/tty 2>/dev/null || true
    printf "\n"

    # Validate if not empty
    if [ -n "$value" ] || [ "$optional" = "false" ]; then
        if ! validate_api_key "$value"; then
            exit 1
        fi
    fi

    # Set variable safely without eval
    case "$var_name" in
        OPENAI_KEY) OPENAI_KEY="$value" ;;
        ANTHROPIC_KEY) ANTHROPIC_KEY="$value" ;;
        *) err "Unknown variable: $var_name"; exit 1 ;;
    esac
}

# ==============================================================================
# Environment File Management
# ==============================================================================

set_env_kv() {
    key="$1"
    value="$2"
    env_file="${3:-.env}"

    # Create .env from .env.example if it doesn't exist
    if [ ! -f "$env_file" ]; then
        env_dir=$(dirname "$env_file")
        if [ -f "${env_dir}/.env.example" ]; then
            cp "${env_dir}/.env.example" "$env_file"
        else
            : > "$env_file"
        fi
        chmod 600 "$env_file" 2>/dev/null || true
    fi

    # Use temp file for atomic write
    temp_file=$(mktemp)

    # Update or append key=value using awk to avoid sed injection risks
    if grep -q "^${key}=" "$env_file" 2>/dev/null; then
        # Update existing key using awk
        awk -v key="$key" -v value="$value" '
            BEGIN { FS=OFS="=" }
            $1 == key { $2 = value; found=1 }
            { print }
            END { if (!found) print key OFS value }
        ' "$env_file" > "$temp_file"
    else
        # Append new key
        cat "$env_file" > "$temp_file"
        printf '%s=%s\n' "$key" "$value" >> "$temp_file"
    fi

    mv "$temp_file" "$env_file"
}

# ==============================================================================
# Docker Health Check
# ==============================================================================

wait_for_healthy() {
    info "Waiting for ragdb to become healthy..."

    timeout=60
    elapsed=0

    while [ $elapsed -lt $timeout ]; do
        # Prefer Docker's health status for ragdb; fall back to 'running' if no healthcheck.
        status=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' ragdb 2>/dev/null || true)

        if [ "$status" = "healthy" ] || [ "$status" = "running" ]; then
            ok "ragdb is ready"
            return 0
        fi

        sleep 2
        elapsed=$((elapsed + 2))
    done

    warn "ragdb did not report ready after ${timeout}s"
    warn "Check logs with: ${DOCKER_COMPOSE} logs -f ragdb"
    return 0
}

# ==============================================================================
# Main Script
# ==============================================================================

main() {
    echo ""
    echo "${BOLD}========================================${RESET}"
    echo "${BOLD}pgEdge RAG Server - Quickstart Demo${RESET}"
    echo "${BOLD}========================================${RESET}"
    echo ""

    # Check dependencies
    check_dependencies
    echo ""

    # Ensure we never leave the terminal with echo disabled
    trap 'stty echo 2>/dev/null || true' EXIT

    # Create workspace and download files
    WORKDIR="$(mktemp -d 2>/dev/null || mktemp -d -t pgedge-rag-download)"
    info "Creating workspace: ${WORKDIR}"
    mkdir -p "${WORKDIR}"

    info "Downloading files from ${BASE_URL}"
    for f in ${FILES}; do
        info "→ ${f}"
        download "${BASE_URL}/${f}" "${WORKDIR}/${f}"
    done
    ok "Downloads complete"
    echo ""

    # Prompt for API keys
    info "This demo requires an OpenAI API key for embeddings"
    info "Anthropic API key is optional (for Claude completions)"
    echo ""

    prompt_secret "Enter your OpenAI API key" OPENAI_KEY false
    prompt_secret "Enter your Anthropic API key" ANTHROPIC_KEY true

    echo ""

    # Validate at least OpenAI key is provided
    if [ -z "$OPENAI_KEY" ]; then
        err "OpenAI API key is required"
        exit 1
    fi

    # Create/update .env file
    info "Creating .env configuration file..."
    # Ensure newly created files are 0600 by default
    umask 077
    set_env_kv "OPENAI_API_KEY" "$OPENAI_KEY" "${WORKDIR}/.env"
    if [ -n "$ANTHROPIC_KEY" ]; then
        set_env_kv "ANTHROPIC_API_KEY" "$ANTHROPIC_KEY" "${WORKDIR}/.env"
    fi
    ok ".env file created"
    echo ""

    # Start services
    info "Starting services with Docker Compose..."
    (
        cd "${WORKDIR}"
        if $DOCKER_COMPOSE up -d; then
            ok "Services started successfully"
        else
            err "Failed to start services"
            exit 1
        fi
    )
    echo ""

    # Wait for health checks
    wait_for_healthy
    echo ""

    # Determine published ports (prefer docker compose output; fall back to env/defaults)
    WEB_CLIENT_PORT_PUBLISHED=$(cd "${WORKDIR}" && $DOCKER_COMPOSE port web-client 3000 2>/dev/null | sed 's/.*://' || true)
    RAG_SERVER_PORT_PUBLISHED=$(cd "${WORKDIR}" && $DOCKER_COMPOSE port rag-server 8080 2>/dev/null | sed 's/.*://' || true)
    POSTGRES_PORT_PUBLISHED=$(cd "${WORKDIR}" && $DOCKER_COMPOSE port ragdb 5432 2>/dev/null | sed 's/.*://' || true)

    # Use defaults if port detection failed or returned invalid values
    [ -z "$WEB_CLIENT_PORT_PUBLISHED" ] || [ "$WEB_CLIENT_PORT_PUBLISHED" = "0" ] && WEB_CLIENT_PORT_PUBLISHED=3001
    [ -z "$RAG_SERVER_PORT_PUBLISHED" ] || [ "$RAG_SERVER_PORT_PUBLISHED" = "0" ] && RAG_SERVER_PORT_PUBLISHED=8081
    [ -z "$POSTGRES_PORT_PUBLISHED" ] || [ "$POSTGRES_PORT_PUBLISHED" = "0" ] && POSTGRES_PORT_PUBLISHED=5433

    # Display success message with connection details
    echo "${BOLD}========================================${RESET}"
    echo "${GREEN}${BOLD}pgEdge RAG Server Demo is Running!${RESET}"
    echo "${BOLD}========================================${RESET}"
    echo ""
    echo "${BOLD}Web Client:${RESET}"
    echo "  ${CYAN}http://localhost:${WEB_CLIENT_PORT_PUBLISHED}${RESET}"
    echo "  Interactive UI for testing RAG pipelines"
    echo ""
    echo "${BOLD}RAG Server API:${RESET}"
    echo "  URL: ${CYAN}http://localhost:${RAG_SERVER_PORT_PUBLISHED}${RESET}"
    echo ""
    echo "${BOLD}Available Pipelines:${RESET}"
    echo "  1. ${YELLOW}postgresql-docs${RESET} - PostgreSQL product documentation"
    echo "     POST http://localhost:${RAG_SERVER_PORT_PUBLISHED}/v1/pipelines/postgresql-docs"
    echo ""
    echo "  2. ${YELLOW}pgedge-docs${RESET} - pgEdge Platform documentation"
    echo "     POST http://localhost:${RAG_SERVER_PORT_PUBLISHED}/v1/pipelines/pgedge-docs"
    echo ""
    echo "${BOLD}Example Query:${RESET}"
    echo "  ${DIM}curl -X POST http://localhost:${RAG_SERVER_PORT_PUBLISHED}/v1/pipelines/postgresql-docs \\${RESET}"
    echo "    ${DIM}-H \"Content-Type: application/json\" \\${RESET}"
    echo "    ${DIM}-d '{\"query\": \"How do I create a table?\"}'${RESET}"
    echo ""
    echo "${BOLD}Database Connection:${RESET}"
    echo "  Host: ${YELLOW}localhost:${POSTGRES_PORT_PUBLISHED}${RESET}"
    echo "  Database: ${YELLOW}ragdb${RESET}"
    echo "  User: ${YELLOW}docuser${RESET}"
    echo "  Password: ${YELLOW}docpass${RESET}"
    echo ""
    echo "  ${DIM}psql -h localhost -p ${POSTGRES_PORT_PUBLISHED} -U docuser -d ragdb${RESET}"
    echo "  ${DIM}docker exec -it ragdb psql -U docuser -d ragdb${RESET}"
    echo ""
    echo "${BOLD}========================================${RESET}"
    echo ""
    echo "${BOLD}Workspace:${RESET} ${CYAN}${WORKDIR}${RESET}"
    echo ""
    echo "${BOLD}To stop:${RESET} ${DIM}cd ${WORKDIR} && ${DOCKER_COMPOSE} down${RESET}"
    echo "${BOLD}To restart:${RESET} ${DIM}cd ${WORKDIR} && ${DOCKER_COMPOSE} restart${RESET}"
    echo "${BOLD}To view logs:${RESET} ${DIM}cd ${WORKDIR} && ${DOCKER_COMPOSE} logs -f${RESET}"
    echo ""
    echo "${BOLD}Documentation:${RESET} ${CYAN}https://docs.pgedge.com/pgedge-rag-server${RESET}"
    echo ""
}

# Run main function
main
