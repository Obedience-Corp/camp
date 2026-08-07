#!/usr/bin/env bash
# Deterministic fixture for the triage review tapes and pty checks.
#
# Builds a campaign under a throwaway HOME with a run whose lanes are the ones
# the review flow has to demonstrate: two parked rows (bulk-approvable), one
# terminal row (never covered by a bulk approval), and one consolidation whose
# declared successor does not exist.
#
# Usage:  eval "$(docs/demos/fixtures/triage-review-fixture.sh)"
# Prints the campaign path on stdout as `export TRIAGE_FIXTURE=...` so both
# harnesses bootstrap the same way.
set -euo pipefail

camp_bin="${CAMP_BIN:-camp}"
home_dir="$(mktemp -d /tmp/camp-triage-home.XXXXXX)"
campaign="$home_dir/campaign"

export HOME="$home_dir"

"$camp_bin" init "$campaign" \
    --name triage-review-fixture \
    -d "Fixture for the triage review harness" \
    -m "Demonstrate the lane-first review flow" \
    --type product >/dev/null

cd "$campaign"

mkdir -p workflow/design
seed_design() {
    local slug="$1" title="$2"
    mkdir -p "workflow/design/$slug"
    printf 'version: v1alpha8\nkind: workitem\nid: %s\ntype: design\ntitle: %s\n' \
        "$slug" "$title" > "workflow/design/$slug/.workitem"
    printf '# %s\n\nFixture body.\n' "$title" > "workflow/design/$slug/README.md"
}

seed_design design-observation-boundary "Obey observation boundary"
seed_design design-shared-templates "Shared template sync"
seed_design design-schema-tags "Workitem schema tags and projects"
seed_design design-platform-adoption "Platform adoption and extensibility"

"$camp_bin" triage start >/dev/null

# Two parked rows: the lane a bulk approval may cover.
for slug in design-observation-boundary design-shared-templates; do
    "$camp_bin" triage evidence set "$slug" --no-evidence >/dev/null
    "$camp_bin" triage propose "$slug" --disposition parked \
        --summary "Revisit after the launch-critical lane clears" >/dev/null
done

# One terminal row: a bulk approval must never cover it.
"$camp_bin" triage evidence set design-schema-tags --no-evidence >/dev/null
"$camp_bin" triage propose design-schema-tags --disposition completed \
    --summary "Delivered by festival WS0001; only spec-parity follow-ups remain" >/dev/null

# One consolidation whose declared successor does not exist yet.
cat > /tmp/triage-fixture-rationale.json <<'JSON'
{
  "schema_version": "triage/v1alpha1",
  "summary": "Umbrella: split app compatibility and template strategy into focused owners",
  "anchors_used": ["design-app-compatibility", "design-shared-templates"],
  "confidence": "medium"
}
JSON
"$camp_bin" triage evidence set design-platform-adoption --no-evidence >/dev/null
"$camp_bin" triage propose design-platform-adoption --disposition consolidate \
    --file /tmp/triage-fixture-rationale.json >/dev/null

echo "export TRIAGE_FIXTURE=$campaign"
echo "export TRIAGE_FIXTURE_HOME=$home_dir"
