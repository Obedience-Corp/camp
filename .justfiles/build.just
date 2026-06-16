# Build recipes — local builds, profiles, and cross-platform targets
binary_name := "camp"
bin_dir := "bin"
BUILDTOOL := "go run ./internal/buildutil"
build_tags := env_var_or_default("BUILD_TAGS", "")
version_pkg := "github.com/Obedience-Corp/camp/internal/version"
version := env_var_or_default("VERSION", "dev")
commit := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`
build_date := `date -u +"%Y-%m-%dT%H:%M:%SZ"`
platform_ldflags := "-s -w -X " + version_pkg + ".Version=" + version + " -X " + version_pkg + ".Commit=" + commit + " -X " + version_pkg + ".BuildDate=" + build_date

[private]
default:
    @just --list --unsorted --justfile {{source_file()}}

# ─── Local builds ───────────────────────────────────────────

# Build camp binary (vet + build via buildutil)
[no-cd]
default-build:
    @{{BUILDTOOL}} build

# Quick development build (no vet, just binary)
[no-cd]
quick:
    @{{BUILDTOOL}} build-only

# Full pipeline (clean, build, test, integration)
[no-cd]
full:
    @{{BUILDTOOL}} all

# Clean build artifacts
[no-cd]
clean:
    @{{BUILDTOOL}} clean

# ─── Build profiles ─────────────────────────────────────────

# Build in stable profile (default command surface)
[no-cd]
stable:
    BUILD_TAGS='' just build default-build

# Build in dev profile (includes dev-only commands)
[no-cd]
dev:
    BUILD_TAGS=dev just build default-build

# Quick build in stable profile
[no-cd]
quick-stable:
    BUILD_TAGS='' just build quick

# Quick build in dev profile
[no-cd]
quick-dev:
    BUILD_TAGS=dev just build quick

# Show which commands are in dev vs stable release channels
[no-cd]
profile:
    BUILD_TAGS=dev just build quick
    ./bin/camp build-profile

# ─── Cross-platform targets ─────────────────────────────────

# Build for Linux AMD64
[no-cd]
linux:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -n "{{build_tags}}" ]; then
        GOOS=linux GOARCH=amd64 go build -tags '{{build_tags}}' -ldflags '{{platform_ldflags}}' -o {{bin_dir}}/{{binary_name}}-linux-amd64 ./cmd/camp
    else
        GOOS=linux GOARCH=amd64 go build -ldflags '{{platform_ldflags}}' -o {{bin_dir}}/{{binary_name}}-linux-amd64 ./cmd/camp
    fi

# Build for macOS Intel
[no-cd]
darwin:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -n "{{build_tags}}" ]; then
        GOOS=darwin GOARCH=amd64 go build -tags '{{build_tags}}' -ldflags '{{platform_ldflags}}' -o {{bin_dir}}/{{binary_name}}-darwin-amd64 ./cmd/camp
    else
        GOOS=darwin GOARCH=amd64 go build -ldflags '{{platform_ldflags}}' -o {{bin_dir}}/{{binary_name}}-darwin-amd64 ./cmd/camp
    fi

# Build for macOS Apple Silicon
[no-cd]
darwin-arm64:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -n "{{build_tags}}" ]; then
        GOOS=darwin GOARCH=arm64 go build -tags '{{build_tags}}' -ldflags '{{platform_ldflags}}' -o {{bin_dir}}/{{binary_name}}-darwin-arm64 ./cmd/camp
    else
        GOOS=darwin GOARCH=arm64 go build -ldflags '{{platform_ldflags}}' -o {{bin_dir}}/{{binary_name}}-darwin-arm64 ./cmd/camp
    fi

# Build for Windows AMD64
[no-cd]
windows:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -n "{{build_tags}}" ]; then
        GOOS=windows GOARCH=amd64 go build -tags '{{build_tags}}' -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-windows-amd64.exe ./cmd/camp
    else
        GOOS=windows GOARCH=amd64 go build -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-windows-amd64.exe ./cmd/camp
    fi

# Build for all platforms
[no-cd]
platforms: linux darwin darwin-arm64 windows
    @echo "All platform builds complete"

# Cross-platform builds in stable profile
[no-cd]
platforms-stable:
    BUILD_TAGS='' just build platforms

# Cross-platform builds in dev profile
[no-cd]
platforms-dev:
    BUILD_TAGS=dev just build platforms
