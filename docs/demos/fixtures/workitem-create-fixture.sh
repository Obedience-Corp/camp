#!/usr/bin/env bash
# Build a disposable CAMP_VHS_ROOT for camp workitem create, shared by
# docs/demos/workitem-create.tape and `just tui pty-workitem-create`.
#
# Layout:
#   $root/bin/camp                                 the branch binary
#   $root/home/                                    fake HOME pinned to the dark palette
#   $root/camp/                                    an empty camp
#   $root/camp/workflow/explore/first-spike/notes  inside an existing explore item
#
# Usage:
#   FIXTURE=$(mktemp -d)
#   docs/demos/fixtures/workitem-create-fixture.sh "$FIXTURE" ./bin/camp
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/workitem-create.tape
set -euo pipefail

root="${1:?fixture root}"
binary="${2:?camp binary}"

mkdir -p "$root/bin" "$root/home"
cp "$binary" "$root/bin/camp"
chmod +x "$root/bin/camp"

export HOME="$root/home"
export PATH="$root/bin:$PATH"
unset NO_COLOR CLICOLOR || true

camp init "$root/camp" \
    --name workitem-create-demo --no-git --no-register --no-skills --force \
    -d "Disposable fixture for camp workitem create" \
    -m "Prove create infers the type from where it runs" >/dev/null

camp settings set global.theme dark >/dev/null

(cd "$root/camp/workflow" && mkdir -p explore && cd explore && camp workitem create first-spike >/dev/null)
mkdir -p "$root/camp/workflow/explore/first-spike/notes"

printf '%s\n' "$root"
