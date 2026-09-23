#!/usr/bin/env bash
# Create a disposable camp whose submodules pull from local remotes that answer
# after a fixed delay, so `camp pull all` shows its live progress the way it
# does against a real network. Nothing touches the user's config or registry.
# Usage: eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/pull-all-progress-fixture.sh)"
set -euo pipefail

camp_bin="${CAMP_BIN:-camp}"
fixture_root="$(mktemp -d "${TMPDIR:-/tmp}/camp-pull-progress.XXXXXX")"
campaign="$fixture_root/camp"
remotes="$fixture_root/remotes"
seeds="$fixture_root/seeds"
bin="$fixture_root/bin"
mkdir -p "$remotes" "$seeds" "$bin"

export XDG_CONFIG_HOME="$fixture_root/config"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_CONFIG_COUNT=2
export GIT_CONFIG_KEY_0=protocol.ext.allow GIT_CONFIG_VALUE_0=always
export GIT_CONFIG_KEY_1=protocol.file.allow GIT_CONFIG_VALUE_1=always
export GIT_AUTHOR_NAME=Demo GIT_COMMITTER_NAME=Demo
export GIT_AUTHOR_EMAIL=demo@example.com GIT_COMMITTER_EMAIL=demo@example.com
unset CAMP_ROOT

# slow-remote stands in for the network round trip: the ext:: transport runs it
# in place of ssh, it waits, then serves the local bare repository.
cat > "$bin/slow-remote" <<'SCRIPT'
#!/usr/bin/env bash
sleep "$3"
exec git "$1" "$2"
SCRIPT
chmod +x "$bin/slow-remote"

"$camp_bin" init "$campaign" --name pull-demo \
    --description "Pull progress demo" --mission "Show camp pull all" \
    --no-register --no-skills </dev/null >/dev/null
cd "$campaign"
git branch -M main

# name:delay-seconds, in .gitmodules order.
repos="api:1.4 web:0.8 cli:2.1 docs:0.6 sdk-go:1.7 billing:1.1 search:2.4 auth:0.9 mobile:1.9 analytics:1.3 infra:0.7 design:1.6"
for entry in $repos; do
    name="${entry%%:*}"
    git init -q --bare --initial-branch=main "$remotes/$name.git"
    git clone -q "$remotes/$name.git" "$seeds/$name" 2>/dev/null
    printf '# %s\n' "$name" > "$seeds/$name/README.md"
    git -C "$seeds/$name" add README.md
    git -C "$seeds/$name" commit -q -m "initial $name"
    git -C "$seeds/$name" push -q origin main
    git submodule add -q "$remotes/$name.git" "projects/$name"
done
git add -A
git commit -q -m "Add projects"

for entry in $repos; do
    name="${entry%%:*}"
    delay="${entry##*:}"
    git -C "projects/$name" remote set-url origin \
        "ext::$bin/slow-remote %s $remotes/$name.git $delay"
    git -C "projects/$name" branch -q --set-upstream-to=origin/main main
done

# Remote work to pull.
for name in api cli sdk-go search mobile design; do
    printf 'update\n' > "$seeds/$name/CHANGELOG.md"
    git -C "$seeds/$name" add CHANGELOG.md
    git -C "$seeds/$name" commit -q -m "update $name"
    git -C "$seeds/$name" push -q origin main
done

# billing diverged: a local commit plus a remote one, so --ff-only refuses.
printf 'local\n' > projects/billing/LOCAL.md
git -C projects/billing add LOCAL.md
git -C projects/billing commit -q -m "local billing change"
printf 'remote\n' > "$seeds/billing/REMOTE.md"
git -C "$seeds/billing" add REMOTE.md
git -C "$seeds/billing" commit -q -m "remote billing change"
git -C "$seeds/billing" push -q origin main

# infra is parked on a detached HEAD, so it is skipped.
git -C projects/infra checkout -q --detach

printf 'export PULL_PROGRESS_FIXTURE=%q\n' "$campaign"
printf 'export XDG_CONFIG_HOME=%q\n' "$XDG_CONFIG_HOME"
printf 'export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1\n'
printf 'export GIT_CONFIG_COUNT=2\n'
printf 'export GIT_CONFIG_KEY_0=protocol.ext.allow GIT_CONFIG_VALUE_0=always\n'
printf 'export GIT_CONFIG_KEY_1=protocol.file.allow GIT_CONFIG_VALUE_1=always\n'
printf 'export GIT_AUTHOR_NAME=Demo GIT_COMMITTER_NAME=Demo\n'
printf 'export GIT_AUTHOR_EMAIL=demo@example.com GIT_COMMITTER_EMAIL=demo@example.com\n'
printf 'unset CAMP_ROOT\n'
