# Terminal-harness verification for camp's interactive surfaces.
#
# Green unit tests do not verify a TUI. These targets drive the real binary in
# a terminal and inspect what a user would actually see, per
# docs/tribal-knowledge/verify-tui-changes-in-a-pty.md.
#
# Two lanes, and both are needed:
#   pty    asserts state a recording cannot — what landed in the run files
#   tape   renders the flow so a human can look at it
#
# Run everything: just tui all

set positional-arguments

# Recipes run from the module's directory by default, so every one of them
# anchors at the repo root first. Paths in this file are repo-relative.
root := justfile_directory()

[private]
default:
    @just --list --justfile {{source_file()}}

# Verify the tools these targets need.
doctor:
    #!/usr/bin/env bash
    set -euo pipefail
    for tool in python3 vhs ttyd ffmpeg; do
        command -v "$tool" >/dev/null 2>&1 || {
            echo "tui: missing required tool: $tool" >&2
            exit 1
        }
    done
    echo "tui: toolchain ok"

# Provision the throwaway venv the pty checks need (pyte only).
[private]
venv:
    #!/usr/bin/env bash
    set -euo pipefail
    venv_dir="${TMPDIR:-/tmp}/camp-tui-venv"
    if [ ! -x "$venv_dir/bin/python" ]; then
        python3 -m venv "$venv_dir"
        "$venv_dir/bin/pip" install --quiet pyte
    fi
    echo "$venv_dir"

# Run the pty checks for the triage review flow.
#
# Asserts the three things spec doc 07 says a recording cannot: the verdict
# reaches decisions.jsonl, a reopen resumes at the first undecided row, and a
# lane approval never covers a terminal row.
pty-triage-review:
    #!/usr/bin/env bash
    set -euo pipefail
    cd "{{root}}"
    just build-camp >/dev/null
    venv_dir="$(just tui venv)"
    "$venv_dir/bin/python" tests/tui/triage_review_pty.py bin/camp

# Run every pty check.
pty: pty-triage-review

# Record one tape. The tape owns its output path and its fixture bootstrap.
tape tape_path:
    #!/usr/bin/env bash
    set -euo pipefail
    cd "{{root}}"
    just build-camp >/dev/null
    vhs "{{tape_path}}"

# Record the triage review tape suite.
tapes-triage-review:
    #!/usr/bin/env bash
    set -euo pipefail
    cd "{{root}}"
    just build-camp >/dev/null
    for tape in docs/demos/triage-review-*.tape; do
        echo "=== $tape ==="
        vhs "$tape"
    done

# Everything: the assertions first, then the recordings.
all: pty tapes-triage-review
