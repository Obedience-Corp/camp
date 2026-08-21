#!/usr/bin/env just --justfile
# camp CLI build and development tasks

set dotenv-load := true

# Configuration
binary_name := "camp"
bin_dir := "bin"
gobin := env_var_or_default("GOBIN", `go env GOPATH` + "/bin")
version_pkg := "github.com/Obedience-Corp/camp/internal/version"
version := env_var_or_default("VERSION", `git describe --tags --always --dirty 2>/dev/null || echo "dev"`)
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

[doc('Record terminal workflows with VHS')]
mod vhs '.justfiles/vhs.just'

[doc('Verify interactive surfaces in a terminal harness (pty checks + tapes)')]
mod tui '.justfiles/tui.just'

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

# Run golangci-lint against new branch issues (includes vet, staticcheck, errcheck, and more).
# The repository still has historical default-linter findings, so this gate
# blocks new regressions relative to origin/main while that backlog is migrated.
lint:
    #!/usr/bin/env sh
    set -eu
    base="${GOLANGCI_LINT_BASE:-origin/main}"
    if git rev-parse --verify "$base" >/dev/null 2>&1; then
        golangci-lint run --new-from-merge-base "$base" ./...
    else
        echo "lint: $base not found; falling back to golangci-lint --new"
        golangci-lint run --new ./...
    fi
    just lint-no-host-fs-tests
    just lint-no-fmt-errorf
    just lint-no-raw-userhomedir
    go vet -tags=integration ./...

# Reject NEW host-side filesystem-mutating test patterns outside
# tests/integration/. Standing rule (feedback_docker_integration_tests.md):
# exec.Command("git", ...) and similar must run through the containerized
# harness, not host t.TempDir.
#
# Detection covers direct git exec AND helper usage (runGit, mustRunGit,
# initTestRepo, exec.Command(args[0]) wrappers, …). Files that only call
# those helpers were invisible to the old grep; that hole is the point of
# the scanner. Allowlist: internal/hostfslint/allowlist.go.
#
# A test that mutates real filesystem state is compliant when it cannot run
# on a developer's machine. There are two ways to achieve that, and the rule
# accepts both (decision D007):
#
#   1. it lives under tests/integration/ and drives the camp binary, or
#   2. it carries the container_fs build tag, so a plain `just test` does
#      not compile it, and the integration lane cross-compiles it and runs
#      it inside a pooled container (tests/integration/containerfs_test.go).
lint-no-host-fs-tests:
    go run ./internal/hostfslint

# Reject NEW fmt.Errorf occurrences in production code outside tools/.
# This is a count ratchet against the merge-base with origin/main, so legacy
# files can be burned down incrementally but cannot accumulate new uses.
lint-no-fmt-errorf:
    #!/usr/bin/env sh
    set -eu
    base_ref="${FMT_ERRORF_BASE:-origin/main}"
    if ! base_commit=$(git merge-base HEAD "$base_ref" 2>/dev/null); then
        echo "FAIL: cannot find merge-base with $base_ref for fmt.Errorf ratchet"
        echo "Fetch $base_ref or set FMT_ERRORF_BASE to a reachable base ref."
        exit 1
    fi
    files=$(find ./cmd ./internal ./pkg -name '*.go' -not -name '*_test.go' -print 2>/dev/null | sed 's#^\./##' || true)
    new_violators=""
    current_files=0
    for file in $files; do
        current_count=$(grep -o "fmt\\.Errorf" "$file" 2>/dev/null | wc -l | tr -d ' ')
        if [ "$current_count" -eq 0 ]; then
            continue
        fi
        current_files=$((current_files + 1))
        if git cat-file -e "$base_commit:$file" 2>/dev/null; then
            base_count=$(git show "$base_commit:$file" | grep -o "fmt\\.Errorf" | wc -l | tr -d ' ')
        else
            base_count=0
        fi
        if [ "$current_count" -gt "$base_count" ]; then
            new_violators="$new_violators $file:$current_count:$base_count"
        fi
    done
    if [ -n "$new_violators" ]; then
        echo "FAIL: fmt.Errorf count regression in production code (outside tools/):"
        for violation in $new_violators; do
            file=$(printf "%s" "$violation" | cut -d: -f1)
            current_count=$(printf "%s" "$violation" | cut -d: -f2)
            base_count=$(printf "%s" "$violation" | cut -d: -f3)
            echo "  $file ($current_count current, $base_count at $base_ref merge-base)"
        done
        echo ""
        echo "Use camperrors.Wrap / camperrors.Wrapf instead."
        exit 1
    fi
    echo "lint-no-fmt-errorf: clean (no fmt.Errorf count regressions; $current_files current file(s) at/below merge-base baseline)"

# Reject os.UserHomeDir in production code outside internal/pathutil/home.go.
#
# os.UserHomeDir returns ("", err) for an unset HOME and hands back a
# whitespace-only HOME as a valid answer. Joining state paths onto either
# produces a RELATIVE path, so camp writes ~/.obey state into whatever directory
# it was invoked from and cannot find it again from anywhere else. That shipped
# as "camp create succeeded but camp list is empty".
#
# pathutil.Home is the single resolver that rejects both. Unlike the other two
# lint rules this is absolute, not a ratchet: there are zero violators and no
# allowlist, because there is no case where raw os.UserHomeDir is the right call.
lint-no-raw-userhomedir:
    #!/usr/bin/env sh
    set -eu
    allowed="internal/pathutil/home.go"
    violators=$(find ./cmd ./internal ./pkg -name '*.go' -not -name '*_test.go' -print 2>/dev/null \
        | sed 's#^\./##' \
        | while read -r file; do
            [ "$file" = "$allowed" ] && continue
            if grep -q "os\.UserHomeDir" "$file" 2>/dev/null; then
                echo "$file"
            fi
        done)
    if [ -n "$violators" ]; then
        echo "FAIL: os.UserHomeDir in production code outside $allowed:"
        echo "$violators" | sed 's/^/  /'
        echo ""
        echo "Use pathutil.Home() instead. It fails on unset AND whitespace-only"
        echo "HOME, so a state path can never resolve relative to the cwd."
        exit 1
    fi
    echo "lint-no-raw-userhomedir: clean (only $allowed resolves the home directory)"

# Verify the fmt.Errorf ratchet rejects a new production violator.
test-ratchet:
    #!/usr/bin/env sh
    set -eu
    tmpfile=./internal/tmptest_fmt_errorf_ratchet.go
    cleanup() { rm -f "$tmpfile"; }
    trap cleanup EXIT INT TERM
    printf 'package internal\nimport "fmt"\nfunc _tmp() error { return fmt.Errorf("bad") }\n' > "$tmpfile"
    if just lint-no-fmt-errorf 2>&1 | grep -q "FAIL"; then
        echo "ratchet test: PASS (correctly rejected new fmt.Errorf)"
    else
        echo "ratchet test: FAIL (should have rejected new fmt.Errorf)"
        exit 1
    fi

# Lightweight on-demand smoke check, faster than gate-fast: whitespace,
# stable/dev build, vet, lint, and the short dev-profile test subset.
# Run it for a quick signal before pushing. See also: just gate-fast, just gate.
gate-push:
    #!/usr/bin/env sh
    set -eu
    echo "=== gate-push: whitespace ==="
    git diff --check
    echo "=== gate-push: stable build ==="
    just build-camp
    echo "=== gate-push: dev build ==="
    BUILD_TAGS=dev just build-camp
    echo "=== gate-push: vet stable ==="
    go vet ./...
    echo "=== gate-push: vet dev ==="
    go vet -tags dev ./...
    echo "=== gate-push: vet integration ==="
    go vet -tags=integration ./...
    echo "=== gate-push: lint stable ==="
    just lint
    echo "=== gate-push: lint dev ==="
    BUILD_TAGS=dev just lint
    echo "=== gate-push: dev short tests ==="
    go test -short -tags dev ./...
    echo "=== gate-push: PASSED ==="

# Run both-profile builds, vet (stable/dev/integration), lint both profiles, and dev unit tests.
# Use this before moving a PR out of draft or when touching broad behavior. See also: just gate.
gate-fast:
    #!/usr/bin/env sh
    set -eu
    echo "=== gate-fast: stable build ==="
    just build-camp
    echo "=== gate-fast: dev build ==="
    BUILD_TAGS=dev just build-camp
    echo "=== gate-fast: install over a running binary (ETXTBSY regression) ==="
    just install test-live-overwrite
    echo "=== gate-fast: vet stable ==="
    go vet ./...
    echo "=== gate-fast: vet dev ==="
    go vet -tags dev ./...
    echo "=== gate-fast: vet integration ==="
    go vet -tags=integration ./...
    echo "=== gate-fast: lint stable ==="
    just lint
    echo "=== gate-fast: lint dev ==="
    BUILD_TAGS=dev just lint
    echo "=== gate-fast: unit tests dev (the profile festival-app ships) ==="
    BUILD_TAGS=dev just test unit
    echo "=== gate-fast: PASSED ==="

# Run the full both-profile gate: gate-fast plus stable unit tests (C-2 matrix).
# Required by release recipes before tagging; the per-sequence closing command for sequences 05-12.
# No GitHub Actions: all enforcement is local (C-1).
gate:
    #!/usr/bin/env sh
    set -eu
    just gate-fast
    echo "=== gate: unit tests stable ==="
    just test unit
    echo "=== gate: PASSED ==="

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
docs:
    BUILD_TAGS='' just build-camp
    ./{{bin_dir}}/{{binary_name}} gendocs --output docs/cli-reference --format markdown --single

# Run camp (for development)
run *ARGS:
    go run ./cmd/camp {{ARGS}}
