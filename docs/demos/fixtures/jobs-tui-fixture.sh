#!/usr/bin/env bash
# Build a disposable CAMP_VHS_ROOT for docs/demos/jobs-tui.tape.
#
# Layout:
#   $CAMP_VHS_ROOT/bin/camp
#   $CAMP_VHS_ROOT/home/campaign/   # fake HOME campaign with seeded failed jobs
#
# Usage:
#   FIXTURE=$(mktemp -d)
#   docs/demos/fixtures/jobs-tui-fixture.sh "$FIXTURE" ./bin/camp
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/jobs-tui.tape
set -euo pipefail

root="${1:?fixture root}"
binary="${2:?camp binary}"

mkdir -p "$root/bin" "$root/home"
cp "$binary" "$root/bin/camp"
chmod +x "$root/bin/camp"

export HOME="$root/home"
export PATH="$root/bin:$PATH"
unset NO_COLOR CLICOLOR || true

camp init "$HOME/campaign" \
  --name jobs-tui-demo \
  --description 'Deferred commit queue browser' \
  --mission 'Inspect and act on queued jobs without copying ids' \
  --no-register --no-skills >/dev/null

# Force the dark brand palette for the recording (truecolor fire accents).
camp settings set global.theme dark >/dev/null
(
  cd "$HOME/campaign"
  camp settings set local.theme_override dark >/dev/null
)

lane="$HOME/campaign/.campaign/cache/jobs/failed/%2E"
mkdir -p "$lane"

# Two failed jobs with distinct enqueue times so CREATED is obviously useful,
# and so j/k navigation plus in-TUI drop are visible without typing an id.
cat >"$lane/0000001.json" <<'EOF'
{
  "id": "job-20260831T140000Z-a1b2",
  "seq": 1,
  "kind": "commit-paths",
  "repo": ".",
  "paths": [".campaign/intents/rename-notes.md"],
  "message": "capture intent: rename notes",
  "created_at": "2026-08-31T14:00:00.000Z",
  "attempts": 1,
  "last_error": "path vanished before the worker could stage it"
}
EOF

cat >"$lane/0000002.json" <<'EOF'
{
  "id": "job-20260831T151500Z-c3d4",
  "seq": 2,
  "kind": "commit-tree",
  "repo": ".",
  "tree": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "parent": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "auto_write": true,
  "created_at": "2026-08-31T15:15:00.000Z",
  "attempts": 3,
  "last_error": "the commit message writer (ob commit) did not finish within 5m0s"
}
EOF

printf '%s\n' "$root"
