# Build recipes
binary_name := "camp"
bin_dir := "bin"
BUILDTOOL := "go run ./internal/buildutil"

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
    GOOS=linux GOARCH=amd64 go build -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-linux-amd64 ./cmd/camp

# Build for macOS Intel
[no-cd]
darwin:
    GOOS=darwin GOARCH=amd64 go build -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-darwin-amd64 ./cmd/camp

# Build for macOS Apple Silicon
[no-cd]
darwin-arm64:
    GOOS=darwin GOARCH=arm64 go build -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-darwin-arm64 ./cmd/camp

# Build for Windows AMD64
[no-cd]
windows:
    GOOS=windows GOARCH=amd64 go build -ldflags '-s -w' -o {{bin_dir}}/{{binary_name}}-windows-amd64.exe ./cmd/camp

# Build for all platforms
[no-cd]
platforms: linux darwin darwin-arm64 windows
    @echo "All platform builds complete"
