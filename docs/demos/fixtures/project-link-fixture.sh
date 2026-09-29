#!/usr/bin/env bash
# Disposable home for the project-link browser tape.
#
# The camp lives at $HOME/campaign. The recording starts there, so the browser
# opens on $HOME and the visible folders are campaign and src.
#
# Usage:
#   FIXTURE=$(mktemp -d)
#   docs/demos/fixtures/project-link-fixture.sh "$FIXTURE" ./bin/camp
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/project-link.tape
set -euo pipefail

root="${1:?fixture root}"
binary="${2:?camp binary}"

mkdir -p "$root/bin" "$root/home"
cp "$binary" "$root/bin/camp"
chmod +x "$root/bin/camp"

export HOME="$root/home"
export PATH="$root/bin:$PATH"
unset NO_COLOR CLICOLOR || true
git config --global user.email demo@example.com
git config --global user.name Demo
git config --global init.defaultBranch main

camp init "$HOME/campaign" \
  --name demo \
  --description 'Link a folder that already lives on this machine' \
  --mission 'Confirm the shortcut, then link' \
  --no-register --no-skills >/dev/null

camp settings set global.theme dark >/dev/null

ledger="$HOME/src/ledger"
notes="$HOME/src/notes"
mkdir -p "$notes"
git init -q "$ledger"
printf 'module ledger\n\ngo 1.25.0\n' >"$ledger/go.mod"
git -C "$ledger" add go.mod
git -C "$ledger" commit -qm initial

(
  cd "$HOME/campaign"
  camp settings set local.theme_override dark >/dev/null
)

printf '%s\n' "$root"
