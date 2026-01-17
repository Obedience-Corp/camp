# Test recipes

[private]
default:
    @just --list --unsorted --justfile {{source_file()}}

# Run all tests
[no-cd]
all:
    go test -race -cover ./...

# Run tests with verbose output
[no-cd]
verbose:
    go test -race -cover -v ./...

# Run tests with coverage report
[no-cd]
coverage:
    go test -race -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    @echo "Coverage report: coverage.html"

# Run short tests only
[no-cd]
short:
    go test -short ./...

# Run benchmarks
[no-cd]
bench:
    go test -bench=. -benchmem ./...

# Run benchmarks with verbose output
[no-cd]
bench-verbose:
    go test -bench=. -benchmem -v ./...

# ============================================
# Integration Tests (require Docker/Colima)
# ============================================

# Run integration tests
[no-cd]
integration:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Building Linux binary for containers..."
    mkdir -p bin/linux
    GOOS=linux GOARCH=amd64 go build -o bin/linux/camp ./cmd/camp
    echo "Running container-based integration tests..."
    DOCKER_HOST="unix://${HOME}/.colima/default/docker.sock" \
    TESTCONTAINERS_RYUK_DISABLED=true \
    go test -v -tags=integration ./tests/integration/... -timeout 10m

# Run integration tests with verbose output
[no-cd]
integration-verbose:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Building Linux binary for containers..."
    mkdir -p bin/linux
    GOOS=linux GOARCH=amd64 go build -o bin/linux/camp ./cmd/camp
    echo "Running container-based integration tests (verbose)..."
    DOCKER_HOST="unix://${HOME}/.colima/default/docker.sock" \
    TESTCONTAINERS_RYUK_DISABLED=true \
    go test -v -tags=integration ./tests/integration/... -timeout 10m 2>&1

# Run a specific integration test
[no-cd]
integration-run TEST:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Building Linux binary for containers..."
    mkdir -p bin/linux
    GOOS=linux GOARCH=amd64 go build -o bin/linux/camp ./cmd/camp
    echo "Running integration test: {{TEST}}"
    DOCKER_HOST="unix://${HOME}/.colima/default/docker.sock" \
    TESTCONTAINERS_RYUK_DISABLED=true \
    go test -v -tags=integration ./tests/integration/... -run "{{TEST}}" -timeout 5m

# Build Linux binary for integration tests (useful for debugging)
[no-cd]
integration-build:
    @echo "Building Linux binary for containers..."
    @mkdir -p bin/linux
    GOOS=linux GOARCH=amd64 go build -o bin/linux/camp ./cmd/camp
    @echo "Built bin/linux/camp"
