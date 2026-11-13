#!/usr/bin/env bash
# Build a disposable CAMP_VHS_ROOT for docs/demos/approvals-tui.tape.
#
# Layout:
#   $CAMP_VHS_ROOT/bin/camp
#   $CAMP_VHS_ROOT/home/campaign/   # fake HOME campaign with pending approvals
#
# The requests are real: each is a standalone WORKFLOW.md advanced with fest to
# a checkpoint, so the browser shows what fest wrote and can act on it. fest
# must be on PATH.
#
# Usage:
#   FIXTURE=$(mktemp -d)
#   docs/demos/fixtures/approvals-tui-fixture.sh "$FIXTURE" ./bin/camp
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/approvals-tui.tape
set -euo pipefail

root="${1:?fixture root}"
binary="${2:?camp binary}"

command -v fest >/dev/null || { echo "fest must be on PATH to build this fixture" >&2; exit 1; }

mkdir -p "$root/bin" "$root/home"
cp "$binary" "$root/bin/camp"
chmod +x "$root/bin/camp"

export HOME="$root/home"
export PATH="$root/bin:$PATH"
unset NO_COLOR CLICOLOR CAMP_ROOT || true
# The disposable HOME has no git identity, and the inbox commits its ledger.
export GIT_AUTHOR_NAME=demo GIT_AUTHOR_EMAIL=demo@example.com
export GIT_COMMITTER_NAME=demo GIT_COMMITTER_EMAIL=demo@example.com

camp init "$HOME/campaign" \
  --name approvals-demo \
  --description 'Human approval inbox' \
  --mission 'Decide what agents are waiting on, in one place' \
  --no-register --no-skills >/dev/null

# Force the dark brand palette for the recording (truecolor fire accents).
camp settings set global.theme dark >/dev/null
(
  cd "$HOME/campaign"
  camp settings set local.theme_override dark >/dev/null
)

# request <dir> <steps-json>: a two-step workflow stopped at its checkpoint.
request() {
  local dir="$HOME/campaign/workflow/explore/$1"
  mkdir -p "$dir"
  (
    cd "$dir"
    export USER=demo LOGNAME=demo
    command fest create workflow "$1" --steps "$2" >/dev/null
    command fest workflow advance >/dev/null   # finish step 1
    command fest workflow advance >/dev/null   # reach the checkpoint
  )
}

request release-notes '{"title":"Release notes","description":"Draft and sign off the v0.19 release notes.","steps":[
 {"name":"DRAFT","goal":"Draft the notes.","actions":["Write them."],"output":"docs/release-notes.md","checkpoint":"none"},
 {"name":"SIGNOFF","goal":"Confirm the v0.19 release notes say what changed for users, in their words.","actions":["Read docs/release-notes.md.","Approve, or reject with what to change."],"output":"A recorded decision","checkpoint":"approval_required"}]}'

request prune-restore '{"title":"Prune restore path","description":"Check the saved-edit restore path before it ships.","steps":[
 {"name":"BUILD","goal":"Implement restore.","actions":["Write it."],"output":"code","checkpoint":"none"},
 {"name":"REVIEW","goal":"Review how camp project prune saves and restores uncommitted edits before the release goes out.","actions":["Remove a merged worktree that has modified, deleted and untracked files.","Check the command prints how to restore them and when they expire.","Run camp project prune --restore and confirm the worktree is back on its branch.","Confirm the edits are uncommitted again and nothing is staged.","Confirm an ignored .env.local came back and is still ignored.","Confirm node_modules was named as deleted and not saved.","Edit a file while the confirmation prompt is open and confirm the worktree is kept.","Run with --discard-dirty on a worktree holding unpushed submodule commits and confirm it is kept.","Read the saved list with camp project prune --saved.","Drop one with --drop and confirm the ref is gone."],"output":"Approve to release, or reject with the step that failed","checkpoint":"approval_required"}]}'

request voice-latency '{"title":"Voice latency","description":"Sign off the latency budget.","steps":[
 {"name":"MEASURE","goal":"Measure.","actions":["Run the bench."],"output":"numbers","checkpoint":"none"},
 {"name":"SIGNOFF","goal":"Accept the measured first-token latency as the budget for the next release.","actions":["Read the benchmark table.","Approve the budget, or reject with the number you want."],"output":"A recorded decision","checkpoint":"approval_required"}]}'

# A request whose source is gone from this machine is still listed, and marked
# as not actionable here.
rm -rf "$HOME/campaign/workflow/explore/voice-latency"

(
  cd "$HOME/campaign"
  camp approvals --plain >/dev/null
)
