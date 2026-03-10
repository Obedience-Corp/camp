#!/usr/bin/env just --justfile
# camp CLI build and development tasks

set dotenv-load := true

# Configuration
binary_name := "camp"
bin_dir := "bin"
gobin := env_var_or_default("GOBIN", `go env GOPATH` + "/bin")
version_pkg := "github.com/Obedience-Corp/camp/internal/version"
version := env_var_or_default("VERSION", "dev")
commit := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`
build_date := `date -u +"%Y-%m-%dT%H:%M:%SZ"`
ldflags := "-X " + version_pkg + ".Version=" + version + " -X " + version_pkg + ".Commit=" + commit + " -X " + version_pkg + ".BuildDate=" + build_date

# Modules
[doc('Build (local, profiles, cross-platform)')]
mod build '.justfiles/build.just'

[doc('Testing (unit, coverage, benchmarks)')]
mod test '.justfiles/test.just'

[doc('Release and versioning')]
mod release '.justfiles/release.just'

[doc('Install camp to $GOBIN (stable, dev, current)')]
mod install '.justfiles/install.just'

[private]
default:
    @echo "camp CLI - Campaign Management Tool"
    @echo ""
    @just --list --unsorted

# Build camp binary (shortcut for `just build default-build`)
build-camp:
    @go run ./internal/buildutil build

# Format Go code
fmt:
    go fmt ./...

# Run go vet
vet:
    go vet ./...

# Run golangci-lint (includes vet, staticcheck, errcheck, and more)
lint:
    golangci-lint run ./...

# Install required development tools
install-tools:
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

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

# Uninstall camp from $GOBIN
uninstall:
    @echo "Uninstalling camp..."
    rm -f {{gobin}}/{{binary_name}}
    @echo "camp uninstalled"

# Generate CLI reference docs
docs: build-camp
    ./{{bin_dir}}/{{binary_name}} gendocs --output docs/cli-reference --format markdown --single

# Run camp (for development)
run *ARGS:
    go run ./cmd/camp {{ARGS}}
