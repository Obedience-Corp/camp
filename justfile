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
BUILDTOOL := "go run ./internal/buildutil"

# Modules
[doc('Testing (unit, coverage, benchmarks)')]
mod test '.justfiles/test.just'

[doc('Cross-platform builds')]
mod xbuild '.justfiles/build.just'

[doc('Release and versioning')]
mod release '.justfiles/release.just'

[doc('Install camp to $GOBIN (stable, dev, current)')]
mod install '.justfiles/install.just'

[private]
default:
    @echo "camp CLI - Campaign Management Tool"
    @echo ""
    @just --list --unsorted

# Build camp binary (vet + build via buildutil)
build:
    @{{BUILDTOOL}} build

# Build camp binary in stable profile (default command surface)
build-stable:
    BUILD_TAGS='' just build

# Build camp binary in dev profile (includes dev-only commands)
build-dev:
    BUILD_TAGS=dev just build

# Quick development build (no vet, just binary)
build-only:
    @{{BUILDTOOL}} build-only

# Build only in stable profile
build-only-stable:
    BUILD_TAGS='' just build-only

# Build only in dev profile
build-only-dev:
    BUILD_TAGS=dev just build-only

# Cross-platform builds in stable profile
xbuild-stable:
    BUILD_TAGS='' just xbuild platforms

# Cross-platform builds in dev profile
xbuild-dev:
    BUILD_TAGS=dev just xbuild platforms

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
docs: build
    ./{{bin_dir}}/{{binary_name}} gendocs --output docs/cli-reference --format markdown --single

# Run camp (for development)
run *ARGS:
    go run ./cmd/camp {{ARGS}}
