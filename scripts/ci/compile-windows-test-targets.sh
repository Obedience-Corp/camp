#!/usr/bin/env bash
# Compile (do not run) Windows amd64 test binaries for selected tag profiles.
# Safe on Linux/macOS hosts: GOOS=windows produces PE binaries that are not executed.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

export CGO_ENABLED=0
export GOOS=windows
export GOARCH=amd64

OUT_DIR="${ROOT}/out/windows-test-compile"
MANIFEST="${OUT_DIR}/manifest.txt"
PARALLEL="${WINDOWS_TEST_COMPILE_PARALLEL:-8}"

usage() {
  cat <<'EOF'
Usage: compile-windows-test-targets.sh [--verify-failure-gate]

  Default: enumerate test packages with go list, compile each with go test -c
  for stable, dev, container_fs, and integration profiles; print counts.

  --verify-failure-gate: assert a deliberate Windows compile error fails (self-test).
EOF
}

pkg_hash() {
  # Stable, filesystem-safe name for a test binary output path.
  printf '%s' "$1" | shasum -a 256 | awk '{print $1}' | cut -c1-20
}

list_test_packages() {
  local tags="$1"
  local scope="$2"
  local -a list_args=(list)
  if [[ -n "$tags" ]]; then
    list_args+=(-tags "$tags")
  fi
  list_args+=(-test -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}')
  # -test lists each package twice (library + test archive); keep library paths only.
  # Capture go list separately so an enumeration error cannot look like zero targets.
  local listed
  listed="$(go "${list_args[@]}" "$scope")" || return 1
  printf '%s\n' "$listed" | awk 'NF && index($0, " [") == 0'
}

compile_profile() {
  local profile="$1"
  local tags="$2"
  local scope="$3"
  local profile_dir="${OUT_DIR}/${profile}"
  mkdir -p "$profile_dir"

  local listed
  listed="$(list_test_packages "$tags" "$scope")" || return 1
  mapfile -t packages <<< "$listed"
  local pkg_count="${#packages[@]}"
  if (( pkg_count == 0 )) || [[ -z "${packages[0]}" ]]; then
    echo "no Windows test packages found for profile=${profile} tags=${tags:-none} scope=${scope}" >&2
    return 1
  fi

  local fail_dir="${profile_dir}/failures"
  mkdir -p "$fail_dir"
  rm -f "${fail_dir}"/*

  compile_one_package() {
    local pkg="$1"
    local out="${profile_dir}/$(pkg_hash "$pkg").test.exe"
    local -a test_args=(test -c)
    if [[ -n "$tags" ]]; then
      test_args+=(-tags "$tags")
    fi
    test_args+=(-o "$out" "$pkg")
    if ! go "${test_args[@]}"; then
      echo "compile failed: ${pkg}" >"${fail_dir}/$(pkg_hash "$pkg").fail"
      return
    fi
    if [[ ! -s "$out" ]]; then
      echo "empty test binary: ${pkg}" >"${fail_dir}/$(pkg_hash "$pkg").fail"
    fi
  }
  export -f compile_one_package pkg_hash
  export profile_dir tags fail_dir

  printf '%s\0' "${packages[@]}" | xargs -0 -P "$PARALLEL" -I {} bash -c 'compile_one_package "$1"' _ {}

  local fail_count compiled
  fail_count="$(find "$fail_dir" -type f -name '*.fail' 2>/dev/null | wc -l | tr -d ' ')"
  compiled=$((pkg_count - fail_count))
  if (( fail_count > 0 )); then
    echo "compile-windows-test-targets: ${fail_count} package(s) failed (profile=${profile})" >&2
    find "$fail_dir" -type f -name '*.fail' -exec head -1 {} \; 2>/dev/null | head -20 >&2 || true
  fi

  echo "profile=${profile} tags=${tags:-none} scope=${scope} packages=${pkg_count} targets=${compiled} goos=${GOOS} goarch=${GOARCH}" | tee -a "$MANIFEST"
  if (( fail_count > 0 )); then
    return 1
  fi
}

verify_failure_gate() {
  local probe_dir
  probe_dir="$(mktemp -d "${TMPDIR:-/tmp}/camp-windows-compile-probe.XXXXXX")"
  trap 'rm -rf "$probe_dir"' RETURN

  cat >"${probe_dir}/go.mod" <<'EOF'
module compile.probe

go 1.25.6

require golang.org/x/sys v0.47.0
EOF

  cat >"${probe_dir}/bad_windows_test.go" <<'EOF'
package compileprobe

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestShouldNotCompileOnWindows(t *testing.T) {
	unix.Flock(0, unix.LOCK_EX)
}
EOF

  (cd "$probe_dir" && go mod tidy)

  echo "=== verify-failure-gate: expect compile error ==="
  if (cd "$probe_dir" && go test -c -o probe.test.exe ./...) 2>"${probe_dir}/stderr.txt"; then
    echo "verify-failure-gate: expected compile failure but go test -c succeeded" >&2
    return 1
  fi
  if ! grep -qE 'undefined: unix\.(Flock|LOCK_)' "${probe_dir}/stderr.txt"; then
    echo "verify-failure-gate: compile failed but not with expected unix symbol error:" >&2
    cat "${probe_dir}/stderr.txt" >&2
    return 1
  fi
  echo "verify-failure-gate: OK (platform-only compile error surfaced)"
}

main_compile() {
  rm -rf "$OUT_DIR"
  mkdir -p "$OUT_DIR"
  : >"$MANIFEST"

  echo "=== compile-windows-test-targets: GOOS=${GOOS} GOARCH=${GOARCH} CGO_ENABLED=${CGO_ENABLED} ==="

  local total_packages=0
  local total_targets=0

  run_profile() {
    local line
    line="$(compile_profile "$@")"
    echo "$line"
    local pkgs targets
    pkgs="$(echo "$line" | sed -n 's/.* packages=\([0-9]*\).*/\1/p')"
    targets="$(echo "$line" | sed -n 's/.* targets=\([0-9]*\).*/\1/p')"
    total_packages=$((total_packages + pkgs))
    total_targets=$((total_targets + targets))
  }

  run_profile stable '' './...'
  run_profile dev dev './...'
  # Linux-oriented container_fs suites stay in the integration lane; Windows CI
  # compiles the starter package exercised by windows-starter-lock.
  run_profile container_fs container_fs './internal/starter/...'
  run_profile integration integration './tests/integration/...'

  echo "=== summary: profiles=4 package-slots=${total_packages} compiled-targets=${total_targets} goos=${GOOS} goarch=${GOARCH} ==="
  echo "manifest: ${MANIFEST}"
}

case "${1:-}" in
  --verify-failure-gate)
    verify_failure_gate
    ;;
  -h | --help)
    usage
    ;;
  '')
    main_compile
    ;;
  *)
    echo "unknown argument: $1" >&2
    usage >&2
    exit 2
    ;;
esac
