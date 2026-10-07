#!/usr/bin/env bash
# Build a disposable CAMP_VHS_ROOT for the camp notify browser, shared by
# docs/demos/notify-browse.tape and `just tui pty-notify`.
#
# Layout:
#   $root/bin/camp                  the branch binary
#   $root/bin/{pbcopy,xclip,...}    stubs that log what they are handed to
#                                   $CAMP_VHS_HANDOFF_LOG instead of the
#                                   operator's clipboard
#   $root/home/                     fake HOME pinned to the dark palette
#   $root/camp/                     three live notices: the legacy dungeon
#                                   layout, a stale workitem link, and two
#                                   never-synced artifact roots the detector
#                                   reports one at a time
#   $root/empty/                    a camp with nothing to report
#
# Usage:
#   FIXTURE=$(mktemp -d)
#   docs/demos/fixtures/notify-fixture.sh "$FIXTURE" ./bin/camp
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/notify-browse.tape
set -euo pipefail

root="${1:?fixture root}"
binary="${2:?camp binary}"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

mkdir -p "$root/bin" "$root/home"
cp "$binary" "$root/bin/camp"
for name in pbcopy xclip xsel wl-copy open xdg-open; do
    cp "$here/record-handoff" "$root/bin/$name"
done
chmod +x "$root/bin"/*

export HOME="$root/home"
export PATH="$root/bin:$PATH"
unset NO_COLOR CLICOLOR || true

for name in camp empty; do
    camp init "$root/$name" \
        --name "notify-$name" --no-git --no-register --no-skills --force \
        -d "Disposable fixture for the notify browser" \
        -m "Prove camp notify dismisses and restores in place" >/dev/null
done

camp settings set global.theme dark >/dev/null

camp="$root/camp"
# The visible spelling is what the legacy-layout detector looks for.
mv "$camp/.dungeon" "$camp/dungeon"
mkdir -p "$camp/data/models" "$camp/data/zoo" "$camp/.campaign/workitems"
echo weights >"$camp/data/models/model.bin"
echo weights >"$camp/data/zoo/zoo.bin"
printf 'version: 1\nroots:\n  - path: data/models\n  - path: data/zoo\n' >"$camp/.campaign/artifacts.yaml"
printf '%s\n' \
    'version: workitem-links/v1alpha1' \
    'links:' \
    '  - id: lnk_20261006_00000a' \
    '    workitem_id: design-gone-2026-10-06' \
    '    scope:' \
    '      kind: campaign_path' \
    '      path: workflow/design/gone' \
    '    role: primary' \
    '    created_at: 2026-10-06T00:00:00Z' \
    '    created_by: fixture' >"$camp/.campaign/workitems/links.yaml"

printf '%s\n' "$root"
