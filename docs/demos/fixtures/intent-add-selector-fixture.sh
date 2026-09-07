#!/usr/bin/env bash
# Disposable campaign for the intent-add selector VHS tape.
#
# Eight projects plus a seeded nav log so `camp` is the recency winner at the
# bottom of the project picker. Fake HOME; never the operator's registry.
#
# Usage: eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/intent-add-selector-fixture.sh)"
set -euo pipefail

camp_bin="${CAMP_BIN:-camp}"
home_dir="$(mktemp -d /tmp/camp-intent-add-selector-home.XXXXXX)"
campaign="$home_dir/campaign"

export HOME="$home_dir"
git config --global user.email demo@example.com
git config --global user.name Demo

"$camp_bin" init "$campaign" \
    --name selector-demo \
    -d "Selector recency demo" \
    -m "Show type-to-filter project picker" \
    --no-register --no-skills >/dev/null

mkdir -p "$campaign/projects"/{agent-simulator,build-util,camp,camp-activity,camp-buzz,fest,obey,zzz-archive}

cache="$campaign/.campaign/cache"
mkdir -p "$cache"
cat >"$cache/state.jsonl" <<EOF
{"location":"$campaign/projects/fest","rel":"projects/fest","ts":"2026-01-01T00:00:00Z","kind":"visit"}
{"location":"$campaign/projects/obey","rel":"projects/obey","ts":"2026-06-01T00:00:00Z","kind":"visit"}
{"location":"$campaign/projects/camp","rel":"projects/camp","ts":"2026-09-01T00:00:00Z","kind":"visit"}
EOF

echo "export INTENT_ADD_SELECTOR_FIXTURE=$campaign"
echo "export INTENT_ADD_SELECTOR_HOME=$home_dir"
echo "export HOME=$home_dir"
