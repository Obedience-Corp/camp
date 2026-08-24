#!/usr/bin/env bash
# Deterministic fixture for the completed-run sweep prompt pty check.
#
# Builds a campaign under a throwaway HOME holding one workitem of each kind the
# prompt has to treat differently:
#
#   workflow/chore/tidy-imports    completed run -> three-way lifecycle select
#   workflow/explore/provider-scan completed run -> the six-way routing select
#   workflow/design/observation    completed run -> never offered, reported
#
# Every content file is stamped well into the past so the fresh-write guard does
# not withhold the items the prompt is supposed to ask about; .workflow/ is left
# at its real time on purpose, because the guard ignores it and stamping it would
# hide a regression in that exclusion.
#
# Usage:  eval "$(docs/demos/fixtures/sweep-prompt-fixture.sh)"
set -euo pipefail

camp_bin="${CAMP_BIN:-camp}"
home_dir="$(mktemp -d /tmp/camp-sweep-home.XXXXXX)"
campaign="$home_dir/campaign"

export HOME="$home_dir"

"$camp_bin" init "$campaign" \
    --name sweep-prompt-fixture \
    -d "Fixture for the completed-run sweep prompt harness" \
    -m "Demonstrate type-aware completion prompts" \
    --type product >/dev/null

cd "$campaign"

# The post-completion shape fest actually writes: active_run_id is cleared and
# the completed run survives in the manifest's runs[] index, its terminal status
# verifiable by replaying its events.
seed_item() {
    local type="$1" slug="$2" title="$3"
    local dir="workflow/$type/$slug"
    mkdir -p "$dir/.workflow/runs/r1"
    printf 'version: v1alpha9\nkind: workitem\nid: %s\ntype: %s\ntitle: %s\n' \
        "$type-$slug" "$type" "$title" > "$dir/.workitem"
    printf '# %s\n\nFixture body.\n' "$title" > "$dir/README.md"
    printf 'workflow_id: wf-%s\nruns:\n    - run_id: r1\n      status: completed\n      ended_at: "2026-07-24T19:00:00Z"\n' \
        "$slug" > "$dir/.workflow/workflow.yaml"
    printf 'status: completed\nsummary:\n  total_steps: 1\n' > "$dir/.workflow/runs/r1/run.yaml"
    cat > "$dir/.workflow/runs/r1/progress_events.jsonl" <<'EVENTS'
{"event_type":"workflow_run_started"}
{"event_type":"wf_step_start"}
{"event_type":"wf_step_done"}
{"event_type":"workflow_run_completed"}
EVENTS
}

seed_item chore tidy-imports "Tidy the import blocks"
seed_item explore provider-scan "Local provider landscape scan"
seed_item design observation "Obey observation boundary"

git add -A >/dev/null
git -c user.email=fixture@example.com -c user.name=Fixture \
    commit -q -m "fixture: seed completed-run workitems" >/dev/null

# Age the authored content past the fresh-write window, leaving .workflow alone.
find workflow -type f -not -path '*/.workflow/*' -exec touch -t 202601010000 {} +

echo "export SWEEP_FIXTURE=$campaign"
echo "export SWEEP_FIXTURE_HOME=$home_dir"
