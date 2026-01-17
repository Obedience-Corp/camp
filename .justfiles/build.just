# Build recipes
binary_name := "camp"
bin_dir := "bin"

[private]
default:
    @just --list --unsorted --justfile {{source_file()}}

# Quick development build (no ldflags)
[no-cd]
dev:
    @mkdir -p {{bin_dir}}
    go build -o {{bin_dir}}/{{binary_name}} ./cmd/camp

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
all: linux darwin darwin-arm64 windows
    @echo "All platform builds complete"
