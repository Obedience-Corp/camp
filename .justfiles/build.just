# Build recipes
binary_name := "camp"
bin_dir := "bin"
BUILDTOOL := "go run ./internal/buildutil"
build_tags := env_var_or_default("BUILD_TAGS", "")

[private]
default:
    @just --list --unsorted --justfile {{source_file()}}

# Build with dashboard (vet + build all packages)
[no-cd]
all:
    @{{BUILDTOOL}} build

# Quick development build (no vet, just binary)
[no-cd]
dev:
    @{{BUILDTOOL}} build-only

# Clean build artifacts
[no-cd]
clean:
    @{{BUILDTOOL}} clean

# Run all tasks (clean, build, test, integration)
[no-cd]
full:
    @{{BUILDTOOL}} all

# Build for Linux AMD64
[no-cd]
linux:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -n "{{build_tags}}" ]; then
        GOOS=linux GOARCH=amd64 go build -tags '{{build_tags}}' -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-linux-amd64 ./cmd/camp
    else
        GOOS=linux GOARCH=amd64 go build -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-linux-amd64 ./cmd/camp
    fi

# Build for macOS Intel
[no-cd]
darwin:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -n "{{build_tags}}" ]; then
        GOOS=darwin GOARCH=amd64 go build -tags '{{build_tags}}' -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-darwin-amd64 ./cmd/camp
    else
        GOOS=darwin GOARCH=amd64 go build -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-darwin-amd64 ./cmd/camp
    fi

# Build for macOS Apple Silicon
[no-cd]
darwin-arm64:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ -n "{{build_tags}}" ]; then
        GOOS=darwin GOARCH=arm64 go build -tags '{{build_tags}}' -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-darwin-arm64 ./cmd/camp
    else
        GOOS=darwin GOARCH=arm64 go build -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-darwin-arm64 ./cmd/camp
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
