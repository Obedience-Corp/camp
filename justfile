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
    @just lint-no-host-fs-tests

# Reject NEW host-side filesystem-mutating test patterns outside
# tests/integration/. Standing rule (feedback_docker_integration_tests.md):
# exec.Command("git", ...) and similar must run through the containerized
# harness, not host t.TempDir.
#
# The allowlist captures the pre-existing violators tracked under
# CW0003-tests-01 and follow-up migration scope. Adding a NEW file to the
# violator set fails the build. Removing a file from the allowlist as you
# migrate it tightens the rule. The end state is an empty allowlist.
lint-no-host-fs-tests:
    #!/usr/bin/env sh
    set -eu
    # Allowlist: existing host-FS test violators tracked under CW0003-tests-01
    # and adjacent legacy patterns. Goal: drive this list to empty by migrating
    # tests to tests/integration/ + the container harness. The rule blocks any
    # NEW addition beyond this set.
    allowlist="./internal/git/remote_test.go ./internal/git/commit_test.go ./internal/git/lock_integration_test.go ./internal/git/info_exclude_test.go ./pkg/commitkit/commitkit_test.go ./internal/commands/workitem/staging_test.go ./internal/commands/workitem/commits_test.go ./cmd/camp/commit_integration_test.go ./cmd/camp/project/remote/remote_test.go ./cmd/camp/refs/commands_integration_test.go ./tools/release-notes/main_test.go ./internal/doctor/checks/orphan_test.go ./internal/doctor/checks/head_test.go ./internal/doctor/checks/url_test.go ./internal/project/add_test.go ./internal/project/resolve_test.go ./internal/project/list_test.go ./internal/attach/attach_test.go ./internal/leverage/backfill_test.go ./internal/leverage/authors_test.go ./internal/leverage/projects_test.go ./internal/leverage/blame_cache_test.go ./internal/leverage/sampler_test.go ./internal/leverage/config_test.go ./internal/clone/git_test.go ./internal/quest/autocommit_integration_test.go ./internal/sync/sync_test.go ./internal/sync/preflight_test.go ./internal/scaffold/init_behavior_test.go ./internal/git/commit/commit_test.go ./internal/git/executor_test.go ./internal/git/submodule_test.go ./internal/git/submodule_list_test.go ./internal/git/branches_test.go ./internal/git/submodule_orphan_test.go ./internal/git/resolve_test.go"
    hits=$(find . -name '*_test.go' -not -path './tests/integration/*' -not -path './vendor/*' -print0 2>/dev/null | \
        xargs -0 grep -lE 'exec\.Command\("git"|exec\.CommandContext\(.*"git"' 2>/dev/null || true)
    new_violators=""
    for hit in $hits; do
        case " $allowlist " in
            *" $hit "*) ;;
            *) new_violators="$new_violators $hit" ;;
        esac
    done
    if [ -n "$new_violators" ]; then
        echo "FAIL: NEW host-side git exec.Command in _test.go outside tests/integration/:"
        for v in $new_violators; do echo "  $v"; done
        echo ""
        echo "Migrate to tests/integration/ via GetSharedContainer + RunCampInDir,"
        echo "or (if intentional pure-logic test) add to the allowlist in justfile."
        exit 1
    fi
    echo "lint-no-host-fs-tests: clean (no NEW violators; $(echo $hits | wc -w | tr -d ' ') legacy files on allowlist)"

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
