#!/bin/sh
# Run only in a disposable container with the current Camp binary at /camp.
set -eu
export GIT_AUTHOR_NAME=Fixture GIT_AUTHOR_EMAIL=fixture@example.invalid
export GIT_COMMITTER_NAME=Fixture GIT_COMMITTER_EMAIL=fixture@example.invalid
export GIT_ALLOW_PROTOCOL=file
git config --global protocol.file.allow always
git config --global init.defaultBranch main
mkdir /tmp/recovery
cd /tmp/recovery
fixture_root=$PWD
repo() {
  git init -q "$1"
  echo fixture > "$1/README"
  git -C "$1" add README
  git -C "$1" commit -qm initial
}
repo leaf
leaf_sha=$(git -C leaf rev-parse HEAD)
repo parent
parent_old=$(git -C parent rev-parse HEAD)
git -C parent checkout -qb feature
git -C parent submodule add -q "$PWD/leaf" child
git -C parent commit -qam 'introduce nested child on feature'
repo source
mkdir -p source/.campaign source/projects
cat > source/.campaign/campaign.yaml <<'YAML'
id: 0592ff4e-e091-48bb-b32e-7aee45e018e9
name: recovery-fixture
type: product
YAML
ln -s /Volumes/old-machine/obey-voice source/projects/obey-voice
git -C source submodule add -q "$PWD/parent" projects/sub-01-parent
git -C source/projects/sub-01-parent checkout -q "$parent_old"
git -C source config -f .gitmodules submodule.projects/sub-01-parent.branch feature
i=2
while [ "$i" -le 54 ]; do
  p=$(printf 'projects/sub-%02d' "$i")
  git -C source config -f .gitmodules "submodule.$p.path" "$p"
  if [ "$i" -eq 54 ]; then
    git -C source config -f .gitmodules "submodule.$p.url" "$PWD/temporarily-missing"
  else
    git -C source config -f .gitmodules "submodule.$p.url" "$PWD/leaf"
  fi
  git -C source update-index --add --cacheinfo "160000,$leaf_sha,$p"
  i=$((i+1))
done
git -C source add .gitmodules .campaign projects/obey-voice projects/sub-01-parent
git -C source commit -qm '54 modules, branch-introduced child, optional link'
repo voice
git -C voice remote add origin git@github.com:Obedience-Corp/obey-voice.git
cat > voice/.camp <<'JSON'
{"version":4,"kind":"project","active_campaign_id":"22222222-2222-4222-8222-222222222222","campaign_ids":["22222222-2222-4222-8222-222222222222"],"project_name":"obey-voice"}
JSON
marker_sha=$(sha256sum voice/.camp)
echo 'Camp binary version:'
/camp version 2>&1 | head -5

for parallel in 4 1; do
  export CAMP_REGISTRY_PATH="/tmp/recovery-registry-$parallel.json"
  set --
  if [ "$parallel" -eq 1 ]; then set -- --parallel 1; fi
  checkout="checkout-$parallel"
  set +e
  /camp clone "$PWD/source" "$checkout" "$@" --json > "clone-$parallel.json" 2>&1
  clone_rc=$?
  set -e
  test "$clone_rc" -eq 3
  test -f "$checkout/projects/sub-01-parent/child/README"
  test ! -e "$checkout/projects/sub-54/.git"
  node - "$parallel" <<'JS'
const fs=require('fs'), assert=require('assert');
const p=process.argv[2];
const raw=fs.readFileSync(`clone-${p}.json`,'utf8');
const x=JSON.parse(raw.split(/\n\{\n  "schema_version"/)[0]);
assert.equal(x.success,false); assert.equal(x.submodules.total,54);
assert.equal(x.submodules.initialized,53); assert.equal(x.submodules.failed,1);
assert.equal(x.optionalLinks.skipped,1);
assert.ok(raw.includes('temporarily-missing'));
assert.ok(x.submodules.results.some(r=>r.path==='projects/sub-01-parent' && r.nested && r.nested.some(c=>c.path==='projects/sub-01-parent/child')));
JS
  node - "$checkout" <<'JS'
const fs=require('fs'),assert=require('assert');
const reg=JSON.parse(fs.readFileSync(process.env.CAMP_REGISTRY_PATH));
assert.ok(Object.values(reg.campaigns).some(c=>c.path===`/tmp/recovery/${process.argv[2]}`));
JS
  (cd "$checkout" && /camp status --show-refs) > "status-human-$parallel.txt"
  echo "parallel=$parallel partial clone exit=3, original cause, nested child, registration, optional skip PASS"
  ln -s "$PWD/leaf" temporarily-missing
  echo user-dirty >> "$checkout/projects/sub-02/README"
  git -C "$checkout/projects/sub-03" checkout -qb local-work
  echo user-commit >> "$checkout/projects/sub-03/README"
  git -C "$checkout/projects/sub-03" add README
  git -C "$checkout/projects/sub-03" commit -qm 'local unpushed work'
  local_sha=$(git -C "$checkout/projects/sub-03" rev-parse HEAD)
  (cd "$checkout" && /camp sync projects/sub-54 --no-drain "$@" --json) > "targeted-$parallel.json" 2>&1
  test -f "$checkout/projects/sub-54/README"
  grep -q user-dirty "$checkout/projects/sub-02/README"
  test "$(git -C "$checkout/projects/sub-03" rev-parse HEAD)" = "$local_sha"
  node - "$parallel" <<'JS'
const fs=require('fs'),assert=require('assert');
const x=JSON.parse(fs.readFileSync(`targeted-${process.argv[2]}.json`));
assert.equal(x.success,true); assert.equal(x.updateResults.total,1); assert.equal(x.updateResults.initialized,1);
JS
  echo "parallel=$parallel targeted sync recovered missing module while local work survived PASS"
  set +e
  (cd "$checkout" && /camp sync --no-drain "$@" --json) > "protected-$parallel.json" 2>&1
  protected_rc=$?
  set -e
  test "$protected_rc" -ne 0
  grep -q user-dirty "$checkout/projects/sub-02/README"
  test "$(git -C "$checkout/projects/sub-03" rev-parse HEAD)" = "$local_sha"
  echo "parallel=$parallel full sync protected dirty and unpushed neighbors PASS"
  git -C "$checkout/projects/sub-02" stash push -q -- README
  git -C "$checkout/projects/sub-03" checkout -q main
  (cd "$checkout" && /camp sync --no-drain "$@" --json) > "full-$parallel.json" 2>&1
  git -C "$checkout" submodule status --recursive > "status-$parallel-1.txt"
  (cd "$checkout" && /camp sync --no-drain "$@" --json) > "rerun-$parallel.json" 2>&1
  git -C "$checkout" submodule status --recursive > "status-$parallel-2.txt"
  cmp "status-$parallel-1.txt" "status-$parallel-2.txt"
  test "$(wc -l < "status-$parallel-2.txt")" -eq 55
  if grep -q '^[-U]' "status-$parallel-2.txt"; then cat "status-$parallel-2.txt"; exit 1; fi
  test "$(grep -c '^+' "status-$parallel-2.txt")" -eq 1
  grep '^+' "status-$parallel-2.txt" | grep -q 'projects/sub-01-parent'
  git -C "$checkout/projects/sub-02" stash pop -q
  git -C "$checkout/projects/sub-03" checkout -q local-work
  grep -q user-dirty "$checkout/projects/sub-02/README"
  test "$(git -C "$checkout/projects/sub-03" rev-parse HEAD)" = "$local_sha"
  echo "parallel=$parallel full and repeated sync stable; user work restored PASS"
  (cd "$checkout" && /camp project repair obey-voice --path "$fixture_root/voice" --apply --json) > "repair-$parallel.json"
  test "$(readlink "$checkout/projects/obey-voice")" = "$PWD/voice"
  test "$(sha256sum voice/.camp)" = "$marker_sha"
  git -C "$checkout" diff -- projects/obey-voice | grep -q "$PWD/voice"
  (cd "$checkout" && /camp project repair obey-voice --undo --json) > "undo-$parallel.json"
  test "$(readlink "$checkout/projects/obey-voice")" = /Volumes/old-machine/obey-voice
  echo "parallel=$parallel optional link repaired and undone without ownership change PASS"
  rm temporarily-missing
done
/camp clone "$PWD/source" skipped --no-register --no-submodules --json > skipped.json 2>&1
node -e 'let x=require("./skipped.json");if(!x.success||x.submodules.total!==0||x.validation.checkStates.allInitialized!=="skipped"||x.optionalLinks.skipped!==1)process.exit(1)'
echo 'intentional --no-submodules and optional-link skip PASS'
