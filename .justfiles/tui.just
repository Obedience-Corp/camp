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
    # The precondition is "pyte imports here", not "a python exists here". A
    # venv whose creation was interrupted leaves bin/python behind without pip
    # or pyte, and an existence check reuses it forever; the checks then fail
    # with ModuleNotFoundError and look like a broken harness.
    if ! "$venv_dir/bin/python" -c "import pyte" >/dev/null 2>&1; then
        rm -rf "$venv_dir"
        python3 -m venv "$venv_dir"
        "$venv_dir/bin/python" -m pip install --quiet pyte
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

# Run the pty checks for the completed-run sweep prompt.
#
# Asserts what a passing unit test cannot: that the six-way routing select
# actually renders every answer, that a design is reported rather than offered,
# that promote moves on disk, acknowledgement/recurrence persist, and Ctrl+C
# ends the run instead of re-asking per workitem.
pty-sweep-prompt:
    #!/usr/bin/env bash
    set -euo pipefail
    cd "{{root}}"
    just build-camp >/dev/null
    venv_dir="$(just tui venv)"
    "$venv_dir/bin/python" tests/tui/sweep_prompt_pty.py bin/camp

# Build the disposable fixture the Tailscale-check recording and pty check
# share: the branch binary, a stub ssh that answers the way Tailscale SSH does
# in check mode, and stub opener/clipboard tools that record what they are
# handed. Prints the fixture path. Never touches a live tailnet.
[private]
machine-check-fixture:
    #!/usr/bin/env bash
    set -euo pipefail
    cd "{{root}}"
    fixture="${CAMP_VHS_ROOT:-$(mktemp -d)}"
    mkdir -p "$fixture/bin" "$fixture/home"
    cp bin/camp "$fixture/bin/camp"
    cp docs/demos/fixtures/ssh-tailscale-check "$fixture/bin/ssh"
    cp docs/demos/fixtures/tailscale "$fixture/bin/tailscale"
    for name in open xdg-open pbcopy xclip; do
        cp docs/demos/fixtures/record-handoff "$fixture/bin/$name"
    done
    chmod +x "$fixture/bin"/*
    : > "$fixture/machines.yaml"
    HOME="$fixture/home" CAMP_MACHINES_PATH="$fixture/machines.yaml" \
        "$fixture/bin/camp" machine add devbox \
        --host devbox.example-net.ts.net --auth tailscale-ssh --user builder >/dev/null
    echo "$fixture"

# Assert what the Tailscale-check recording cannot: the approval URL is intact
# on ONE line in both the detail pane and the hop overlay (a link split across
# a wrap is a link nobody can copy), and o/c hand the exact URL to the platform
# opener and clipboard.
pty-machine-tailscale-check:
    #!/usr/bin/env bash
    set -euo pipefail
    cd "{{root}}"
    just build-camp >/dev/null
    venv_dir="$(just tui venv)"
    fixture="$(CAMP_VHS_ROOT="${CAMP_VHS_ROOT:-}" just tui machine-check-fixture)"
    evidence="${CAMP_TUI_EVIDENCE_DIR:-out/evidence/machine-tailscale-check}"
    CAMP_MACHINES_PATH="$fixture/machines.yaml" \
        "$venv_dir/bin/python" tests/tui/machine_tailscale_check_pty.py "$fixture" "$evidence"

# Run every pty check.
pty: pty-triage-review pty-machine-tailscale-check pty-sweep-prompt

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
