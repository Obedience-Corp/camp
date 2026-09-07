#!/usr/bin/env bash
# Disposable campaign for the project-list selector VHS tape.
#
# Go projects atlas + grok-cli, plus web/tools/notes. Nav log ranks grok-cli
# most recent so the cursor starts there with no j.
#
# Usage: eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/project-list-selector-fixture.sh)"
set -euo pipefail

camp_bin="${CAMP_BIN:-camp}"
home_dir="$(mktemp -d /tmp/camp-project-list-selector-home.XXXXXX)"
campaign="$home_dir/campaign"

export HOME="$home_dir"
git config --global user.email demo@example.com
git config --global user.name Demo

"$camp_bin" init "$campaign" \
    --name project-list-demo \
    -d "Interactive project browser" \
    -m "Find a project and jump" \
    --no-register --no-skills >/dev/null

mkdir -p "$campaign/projects"/{atlas,grok-cli,web,tools,notes}
echo 'module atlas' > "$campaign/projects/atlas/go.mod"
echo 'module grok-cli' > "$campaign/projects/grok-cli/go.mod"
echo '{}' > "$campaign/projects/web/package.json"
echo 'name=tools' > "$campaign/projects/tools/pyproject.toml"
echo notes > "$campaign/projects/notes/README.md"

for p in atlas grok-cli web tools notes; do
    git -C "$campaign/projects/$p" init -q
    git -C "$campaign/projects/$p" add -A
    git -C "$campaign/projects/$p" commit -qm initial
done

cache="$campaign/.campaign/cache"
mkdir -p "$cache"
cat >"$cache/state.jsonl" <<EOF
{"location":"$campaign/projects/atlas","rel":"projects/atlas","ts":"2026-01-01T00:00:00Z","kind":"visit"}
{"location":"$campaign/projects/web","rel":"projects/web","ts":"2026-06-01T00:00:00Z","kind":"visit"}
{"location":"$campaign/projects/grok-cli","rel":"projects/grok-cli","ts":"2026-09-01T00:00:00Z","kind":"visit"}
EOF

echo "export PROJECT_LIST_SELECTOR_FIXTURE=$campaign"
echo "export PROJECT_LIST_SELECTOR_HOME=$home_dir"
echo "export HOME=$home_dir"
