#!/bin/sh
# Run only in a disposable container with /camp mounted to the built binary.
set -eu
export GIT_AUTHOR_NAME=Fixture GIT_AUTHOR_EMAIL=fixture@example.invalid
export GIT_COMMITTER_NAME=Fixture GIT_COMMITTER_EMAIL=fixture@example.invalid
export GIT_ALLOW_PROTOCOL=file
git config --global protocol.file.allow always
git config --global init.defaultBranch main
mkdir -p /tmp/clone-sync-owner-lock
cd /tmp/clone-sync-owner-lock
git init -q leaf
echo fixture > leaf/README
git -C leaf add README
git -C leaf commit -qm initial
git init -q source
mkdir -p source/.campaign
cat > source/.campaign/campaign.yaml <<'EOF'
id: 0592ff4e-e091-48bb-b32e-7aee45e018e9
name: owner-lock-fixture
type: product
EOF
git -C source submodule add -q "$PWD/leaf" projects/leaf
git -C source add .
git -C source commit -qm fixture
/camp clone "$PWD/source" checkout --no-register --no-submodules --json > clone.json
touch checkout/.git/config.lock
start=$(date +%s)
set +e
(cd checkout && timeout 15 /camp sync --no-drain --json) > locked-sync.log 2>&1
status=$?
set -e
duration=$(($(date +%s) - start))
if [ "$status" -eq 0 ] || [ "$status" -eq 124 ]; then
  echo "FAIL: sync with external config lock returned $status after ${duration}s"
  cat locked-sync.log
  exit 1
fi
if [ ! -f checkout/.git/config.lock ]; then
  echo 'FAIL: sync removed an external config lock'
  exit 1
fi
if ! grep -Eqi 'lock|config' locked-sync.log; then
  echo 'FAIL: sync did not report the config lock cause'
  cat locked-sync.log
  exit 1
fi
echo "PASS: external config lock preserved; sync failed in ${duration}s with exit $status"
