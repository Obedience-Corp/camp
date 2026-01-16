#!/usr/bin/env just --justfile
# camp CLI build and development tasks

set dotenv-load := true

# Configuration
binary_name := "camp"
bin_dir := "bin"
gobin := env_var_or_default("GOBIN", `go env GOPATH` + "/bin")
version_pkg := "github.com/obediencecorp/camp/internal/version"
version := env_var_or_default("VERSION", "dev")
commit := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`
build_date := `date -u +"%Y-%m-%dT%H:%M:%SZ"`
ldflags := "-X " + version_pkg + ".Version=" + version + " -X " + version_pkg + ".Commit=" + commit + " -X " + version_pkg + ".BuildDate=" + build_date

[private]
default:
    @echo "camp CLI - Campaign Management Tool"
    @echo ""
    @just --list --unsorted

# Build camp binary
build:
    @echo "Building camp..."
    @mkdir -p {{bin_dir}}
    go build -ldflags '{{ldflags}}' -o {{bin_dir}}/{{binary_name}} ./cmd/camp
    @echo "Built {{bin_dir}}/{{binary_name}}"

# Build camp binary (dev mode, no ldflags)
build-dev:
    @echo "Building camp (dev)..."
    @mkdir -p {{bin_dir}}
    go build -o {{bin_dir}}/{{binary_name}} ./cmd/camp
    @echo "Built {{bin_dir}}/{{binary_name}}"

# Run tests
test:
    go test -race -cover ./...

# Run tests with verbose output
test-verbose:
    go test -race -cover -v ./...

# Run tests with coverage report
test-coverage:
    go test -race -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html
    @echo "Coverage report: coverage.html"

# Format Go code
fmt:
    go fmt ./...

# Run go vet
vet:
    go vet ./...

# Run formatting and vetting
lint: fmt vet
    @echo "Linting complete"

# Clean build artifacts
clean:
    rm -rf {{bin_dir}}
    rm -f coverage.out coverage.html
    @echo "Cleaned build artifacts"

# Update and tidy dependencies
deps:
    go get -u ./...
    go mod tidy

# Tidy dependencies
tidy:
    go mod tidy

# Install camp to $GOBIN
install: build
    @echo "Installing camp..."
    @mkdir -p {{gobin}}
    cp {{bin_dir}}/{{binary_name}} {{gobin}}/{{binary_name}}
    @echo "camp installed to {{gobin}}/{{binary_name}}"

# Uninstall camp from $GOBIN
uninstall:
    @echo "Uninstalling camp..."
    rm -f {{gobin}}/{{binary_name}}
    @echo "camp uninstalled"

# Run camp (for development)
run *ARGS:
    go run ./cmd/camp {{ARGS}}
