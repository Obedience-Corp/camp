#!/usr/bin/env bash
# Disposable camp for the project-rename review tape.
#
# atlas and notes are camp-owned directories. web is a linked checkout.
# The recording renames atlas to atlas-core.
#
# Usage:
#   FIXTURE=$(mktemp -d)
#   docs/demos/fixtures/project-rename-fixture.sh "$FIXTURE" ./bin/camp
#   CAMP_VHS_ROOT=$FIXTURE just vhs record-color docs/demos/project-rename.tape
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
  --name project-rename-demo \
  --description 'Review a project rename' \
  --mission 'Confirm the plan, then rename' \
  --no-register --no-skills >/dev/null

camp settings set global.theme dark >/dev/null

web_src="$HOME/web-src"
git init -q "$web_src"
printf '{}\n' >"$web_src/package.json"
git -C "$web_src" add package.json
git -C "$web_src" commit -qm initial

(
  cd "$HOME/campaign"
  camp settings set local.theme_override dark >/dev/null
  mkdir -p projects/atlas projects/notes
  printf 'module atlas\n' >projects/atlas/go.mod
  printf '# Atlas\n' >projects/atlas/README.md
  printf '# Notes\n' >projects/notes/README.md
  ln -s "$web_src" projects/web
  git add projects/atlas projects/notes projects/web
  git commit -qm 'seed projects'
)

printf '%s\n' "$root"
