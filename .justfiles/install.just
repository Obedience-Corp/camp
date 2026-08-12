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
    mkdir -p "{{gobin}}"
    # Install via temp file + rename rather than cp'ing onto the destination.
    # cp opens the destination O_TRUNC on the existing inode; on Linux that is
    # ETXTBSY ("Text file busy") when the inode is a running executable. That is
    # the case whenever camp installs over a camp that is currently running --
    # notably `camp fresh`, which runs this as a follow-up from a live camp.
    # rename(2) swaps the directory entry instead, leaving the live process on
    # its old inode. macOS allows the plain cp; this keeps one path for both.
    # Regression coverage: `just install test-live-overwrite`.
    tmp="$(mktemp "{{gobin}}/.{{binary_name}}.new.XXXXXX")"
    trap 'rm -f "$tmp"' EXIT
    cp {{bin_dir}}/{{binary_name}} "$tmp"
    chmod 0755 "$tmp"
    mv -f -- "$tmp" "{{gobin}}/{{binary_name}}"
    trap - EXIT
    if [[ "$(uname)" == "Darwin" ]]; then
        echo "Signing camp binary for macOS..."
        codesign --force --sign - "{{gobin}}/{{binary_name}}" 2>/dev/null || \
        echo "Warning: Could not sign binary (non-fatal)"
    fi
    echo "camp installed to {{gobin}}/{{binary_name}}"

# Regression lock for the ETXTBSY bug: installing over a RUNNING executable
# must succeed. Stands up a throwaway $GOBIN, puts a live process on the
# destination path, and drives the real _copy recipe against it. A revert to a
# plain `cp` fails this on Linux (it passes on macOS either way, which is
# exactly why the recipe needs a test rather than a reviewer's memory).
[no-cd]
test-live-overwrite:
    #!/usr/bin/env bash
    set -euo pipefail
    workdir="$(mktemp -d)"
    victim=""
    cleanup() {
        [[ -n "$victim" ]] && kill "$victim" 2>/dev/null || true
        rm -rf "$workdir"
    }
    trap cleanup EXIT
    # A real executable occupying the destination, held open by a live process.
    cp "$(command -v sleep)" "$workdir/{{binary_name}}"
    "$workdir/{{binary_name}}" 60 &
    victim=$!
    sleep 0.2
    before="$(ls -i "$workdir/{{binary_name}}" | awk '{print $1}')"
    if ! kill -0 "$victim" 2>/dev/null; then
        echo "FAIL: victim process did not stay alive; test is not exercising ETXTBSY"
        exit 1
    fi
    GOBIN="$workdir" just install _copy
    after="$(ls -i "$workdir/{{binary_name}}" | awk '{print $1}')"
    if [[ "$before" == "$after" ]]; then
        echo "FAIL: destination inode unchanged ($before) -- not a rename swap"
        exit 1
    fi
    if ! cmp -s {{bin_dir}}/{{binary_name}} "$workdir/{{binary_name}}"; then
        echo "FAIL: installed binary does not match {{bin_dir}}/{{binary_name}}"
        exit 1
    fi
    leftover="$(find "$workdir" -maxdepth 1 -name ".{{binary_name}}.new.*" | wc -l | tr -d ' ')"
    if [[ "$leftover" != "0" ]]; then
        echo "FAIL: $leftover temp file(s) left behind in \$GOBIN"
        exit 1
    fi
    echo "test-live-overwrite: PASS (installed over a running executable; inode $before -> $after)"
