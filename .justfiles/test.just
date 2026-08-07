# Test recipes
BUILDTOOL := "go run ./internal/buildutil"

[private]
default:
    @just --list --unsorted --justfile {{source_file()}}

# Run all tests (unit + integration) with dashboard
[no-cd]
all:
    @{{BUILDTOOL}} test
    @{{BUILDTOOL}} integration

# Run unit tests only with dashboard
[no-cd]
unit:
    @{{BUILDTOOL}} test

# Run tests with verbose output (standard go test)
[no-cd]
verbose:
    go test -race -cover -v ./...

# Run tests with coverage report
[no-cd]
coverage:
    @mkdir -p out
    go test -race -coverprofile=out/coverage.out ./...
    go tool cover -html=out/coverage.out -o out/coverage.html
    @echo "Coverage report: out/coverage.html"

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

# Run integration tests with dashboard
[no-cd]
integration:
    @{{BUILDTOOL}} integration

# Run integration tests with verbose output (standard go test)
#
# The timeout is generous on purpose. A timeout reports as `FAIL <pkg> Ns`
# with no failing assertion anywhere in the output, which reads exactly like a
# broken test and costs real time to diagnose. This lane measured 927s
# (15m26s) on 2026-08-05, so the previous 15m budget had already been overrun
# by the suite it was meant to run.
[no-cd]
integration-verbose:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Building Linux binary for containers..."
    mkdir -p bin/linux
    GOOS=linux GOARCH=$(go env GOARCH) go build -o bin/linux/camp ./cmd/camp
    echo "Running container-based integration tests (verbose)..."
    DOCKER_HOST="unix://${HOME}/.colima/default/docker.sock" \
    TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
    go test -v -tags=integration ./tests/integration/... -timeout 30m 2>&1

# Run a specific integration test
[no-cd]
integration-run TEST:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Building Linux binary for containers..."
    mkdir -p bin/linux
    GOOS=linux GOARCH=$(go env GOARCH) go build -o bin/linux/camp ./cmd/camp
    echo "Running integration test: {{TEST}}"
    DOCKER_HOST="unix://${HOME}/.colima/default/docker.sock" \
    TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
    go test -v -tags=integration ./tests/integration/... -run "{{TEST}}" -timeout 5m

# Build Linux binary for integration tests (useful for debugging)
[no-cd]
integration-build:
    @echo "Building Linux binary for containers..."
    @mkdir -p bin/linux
    GOOS=linux GOARCH=$(go env GOARCH) go build -o bin/linux/camp ./cmd/camp
    @echo "Built bin/linux/camp"
