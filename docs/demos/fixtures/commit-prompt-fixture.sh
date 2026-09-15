#!/usr/bin/env bash
# Create a disposable camp without changing the user's config or registry.
# Usage: eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/commit-prompt-fixture.sh)"
set -euo pipefail

camp_bin="${CAMP_BIN:-camp}"
fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/camp-commit-prompt.XXXXXX")"
campaign="$fixture_root/camp"
export XDG_CONFIG_HOME="$fixture_root/config"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=Demo GIT_COMMITTER_NAME=Demo
export GIT_AUTHOR_EMAIL=demo@example.com GIT_COMMITTER_EMAIL=demo@example.com
unset CAMP_ROOT

"$camp_bin" init "$campaign" --name commit-demo \
    --description "Commit prompt demo" --mission "Show themed commit input" \
    --no-register --no-skills </dev/null >/dev/null
cd "$campaign"
printf 'Community notes\nDraft invitation\n' > docs/community.md
printf 'Release checklist\n' > docs/release.md
"$camp_bin" commit --no-drain -m "Add demo notes" >/dev/null
printf 'Community notes\nJoin our Discord\nShare festival replays\n' > docs/community.md
printf 'Release checklist\nVerify public downloads\n' > docs/release.md

printf 'export COMMIT_PROMPT_FIXTURE=%q\n' "$campaign"
printf 'export XDG_CONFIG_HOME=%q\n' "$XDG_CONFIG_HOME"
printf 'export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1\n'
printf 'export GIT_AUTHOR_NAME=Demo GIT_COMMITTER_NAME=Demo\n'
printf 'export GIT_AUTHOR_EMAIL=demo@example.com GIT_COMMITTER_EMAIL=demo@example.com\n'
printf 'unset CAMP_ROOT\n'
