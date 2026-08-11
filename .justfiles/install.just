# Install recipes — install camp with different profiles
binary_name := "camp"
bin_dir := "bin"
gobin := env_var_or_default("GOBIN", `go env GOPATH` + "/bin")

[private]
default:
    @just --list --unsorted --justfile {{source_file()}}

# Install stable profile (default command surface)
[no-cd]
stable:
    #!/usr/bin/env bash
    set -euo pipefail
    BUILD_TAGS='' just build quick
    just install _copy

# Install dev profile (includes dev-only commands)
[no-cd]
dev:
    #!/usr/bin/env bash
    set -euo pipefail
    BUILD_TAGS=dev just build quick
    just install _copy

# Install from current bin/ (whatever was last built)
[no-cd]
current:
    #!/usr/bin/env bash
    set -euo pipefail
    if [ ! -f {{bin_dir}}/{{binary_name}} ]; then
        echo "Error: no binary found at {{bin_dir}}/{{binary_name}}"
        echo "Run 'just build' or 'just build-only' first"
        exit 1
    fi
    just install _copy

# Copy binary to $GOBIN and sign on macOS
[no-cd, private]
_copy:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Installing camp..."
    mkdir -p {{gobin}}
    # Install via temp file + rename rather than cp'ing onto the destination.
    # cp opens the destination O_TRUNC on the existing inode; on Linux that is
    # ETXTBSY ("Text file busy") when the inode is a running executable -- which
    # it always is here, since `camp fresh` runs this from a running camp.
    # rename(2) swaps the directory entry instead, leaving the live process on
    # its old inode. macOS allows the plain cp; this keeps one path for both.
    tmp="{{gobin}}/.{{binary_name}}.new.$$"
    trap 'rm -f "$tmp"' EXIT
    cp {{bin_dir}}/{{binary_name}} "$tmp"
    chmod 0755 "$tmp"
    mv -f "$tmp" {{gobin}}/{{binary_name}}
    trap - EXIT
    if [[ "$(uname)" == "Darwin" ]]; then
        echo "Signing camp binary for macOS..."
        codesign --force --sign - {{gobin}}/{{binary_name}} 2>/dev/null || \
        echo "Warning: Could not sign binary (non-fatal)"
    fi
    echo "camp installed to {{gobin}}/{{binary_name}}"
