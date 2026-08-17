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

# Report whether this machine can run the integration suite: which Docker
# daemon the suite would use, whether it answers fast enough, what else is
# running on it, and whether another run holds the suite lock. Read-only.
[no-cd]
integration-doctor:
    @{{BUILDTOOL}} integration-doctor

# Create or boot the dedicated camp-itest Colima profile the suite runs on.
# The suite does this on demand too; run it by hand to pay the VM boot before
# a timed run, or to repair the profile after stopping it.
[no-cd]
daemon-start:
    @{{BUILDTOOL}} integration-doctor start

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
    # No DOCKER_HOST here on purpose: the harness resolves the dedicated
    # camp-itest profile itself (and says so), so this lane cannot silently
    # run against the shared daemon while the dashboard lane runs isolated.
    TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
    go test -count=1 -v -tags=integration ./tests/integration/... -timeout 30m 2>&1

# Run a specific integration test
#
# -count=1 because a cached PASS here is the worst kind of green: go's test
# cache will replay a previous result without starting a container, so the run
# reports success for work it never did.
[no-cd]
integration-run TEST:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Building Linux binary for containers..."
    mkdir -p bin/linux
    GOOS=linux GOARCH=$(go env GOARCH) go build -o bin/linux/camp ./cmd/camp
    echo "Running integration test: {{TEST}}"
    TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
    go test -count=1 -v -tags=integration ./tests/integration/... -run "{{TEST}}" -timeout 5m

# Build Linux binary for integration tests (useful for debugging)
[no-cd]
integration-build:
    @echo "Building Linux binary for containers..."
    @mkdir -p bin/linux
    GOOS=linux GOARCH=$(go env GOARCH) go build -o bin/linux/camp ./cmd/camp
    @echo "Built bin/linux/camp"
