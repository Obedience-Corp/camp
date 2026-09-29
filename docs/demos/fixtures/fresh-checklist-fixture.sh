#!/usr/bin/env bash
# Disposable campaign for the camp fresh checklist recording.
#
# The project is already on its default branch, behind origin by three
# commits, with four merged local branches whose names are long enough to
# show that prune lists them instead of wrapping one status line. A follow-up
# pauses, then prints three short lines, so the recording shows the spinner.
# A completed design is left in place so the sweep
# reports why it did not move.
#
# Usage: eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/fresh-checklist-fixture.sh)"
set -euo pipefail

camp_bin="${CAMP_BIN:-camp}"
home_dir="$(mktemp -d "${TMPDIR:-/tmp}/camp-fresh-checklist.XXXXXX")"
campaign="$home_dir/campaign"
remote="$home_dir/remotes/ledger.git"
builder="$home_dir/builder"

export HOME="$home_dir"
mkdir -p "$home_dir/bin"
cp "$camp_bin" "$home_dir/bin/camp"
chmod +x "$home_dir/bin/camp"

git config --global user.email demo@example.com
git config --global user.name Demo
git config --global init.defaultBranch main
git config --global protocol.file.allow always

"$camp_bin" init "$campaign" \
    --name fresh-checklist \
    -d "Checklist recording" \
    -m "Sync, prune, and report" \
    --no-register --no-skills >/dev/null

mkdir -p "$(dirname "$remote")" "$builder"
git -C "$builder" init -q
git -C "$builder" commit -q --allow-empty -m base
base="$(git -C "$builder" rev-parse HEAD)"

for branch in festival-starter-camp fix-leverage-worktrees fix-worktree-remove-submodules perf/pull-all-parallel; do
    git -C "$builder" checkout -q -b "$branch"
    git -C "$builder" commit -q --allow-empty -m "$branch"
    git -C "$builder" checkout -q main
    git -C "$builder" merge -q --no-ff "$branch" -m "merge $branch"
done
for n in 1 2 3; do
    git -C "$builder" commit -q --allow-empty -m "origin $n"
done

git -C "$builder" init --bare -q "$remote"
git -C "$builder" remote add origin "$remote"
git -C "$builder" push -q --all origin

git -C "$campaign" -c protocol.file.allow=always submodule add -q "$remote" projects/ledger
git -C "$campaign/projects/ledger" checkout -q -B main "$base"
for branch in festival-starter-camp fix-leverage-worktrees fix-worktree-remove-submodules perf/pull-all-parallel; do
    git -C "$campaign/projects/ledger" branch --no-track "$branch" "origin/$branch" >/dev/null
done

(
    cd "$campaign"
    "$camp_bin" fresh configure add install --run "sleep 1.2; printf '    %s\n' 'built camp' 'signed' 'installed'" >/dev/null
)

seed="$campaign/workflow/design/camp-immerse-color-profiles"
mkdir -p "$seed/.workflow/runs/r1"
printf 'version: v1alpha9\nkind: workitem\nid: design-color-profiles\ntype: design\ntitle: Camp immerse color profiles\n' \
    > "$seed/.workitem"
printf '# Camp immerse color profiles\n\nFixture body.\n' > "$seed/README.md"
printf 'workflow_id: wf-color-profiles\nruns:\n    - run_id: r1\n      status: completed\n      ended_at: "2026-07-24T19:00:00Z"\n' \
    > "$seed/.workflow/workflow.yaml"
printf 'status: completed\nsummary:\n  total_steps: 1\n' > "$seed/.workflow/runs/r1/run.yaml"
cat > "$seed/.workflow/runs/r1/progress_events.jsonl" <<'EVENTS'
{"event_type":"workflow_run_started"}
{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"workflow_run_completed"}
EVENTS
find "$seed" -type f -not -path '*/.workflow/*' -exec touch -t 202601010000 {} +

echo "export FRESH_CHECKLIST_HOME=$home_dir"
echo "export FRESH_CHECKLIST_CAMPAIGN=$campaign"
echo "export FRESH_CHECKLIST_PROJECT=$campaign/projects/ledger"
