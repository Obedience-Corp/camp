#!/bin/sh
# Run only in a disposable container with /camp mounted to the built binary.
set -eu
export GIT_AUTHOR_NAME=Fixture GIT_AUTHOR_EMAIL=fixture@example.invalid
export GIT_COMMITTER_NAME=Fixture GIT_COMMITTER_EMAIL=fixture@example.invalid
export GIT_ALLOW_PROTOCOL=file
git config --global protocol.file.allow always
git config --global init.defaultBranch main
mkdir -p /tmp/sync-repository-identity
cd /tmp/sync-repository-identity
git init -q leaf
echo fixture > leaf/README
git -C leaf add README
git -C leaf commit -qm initial
git init -q source
mkdir -p source/.campaign
cat > source/.campaign/campaign.yaml <<'EOF'
id: 0592ff4e-e091-48bb-b32e-7aee45e018e9
name: identity-fixture
type: product
EOF
echo root > source/README
git -C source submodule add -q "$PWD/leaf" projects/missing
git -C source submodule add -q "$PWD/leaf" projects/dirty
git -C source add .
git -C source commit -qm fixture
/camp clone "$PWD/source" checkout --no-register --no-submodules --json > clone.json
git -C checkout submodule update --init projects/dirty >/dev/null 2>&1
if [ ! -d checkout/.git ] || [ ! -f checkout/projects/dirty/.git ]; then
  echo 'FAIL: fixture did not cover both directory and file Git entries'
  exit 1
fi
echo root-change >> checkout/README
echo local-change >> checkout/projects/dirty/README

# An empty child must not inherit the dirty parent or its branch history.
(cd checkout && /camp sync projects/missing --dry-run --no-drain --json) > targeted-dry.json 2>&1
if [ -e checkout/projects/missing/.git ]; then
  echo 'FAIL: dry run initialized the missing child'
  exit 1
fi
(cd checkout && /camp sync projects/missing --no-drain --json) > targeted-sync.json 2>&1
if [ ! -e checkout/projects/missing/.git ]; then
  echo 'FAIL: targeted sync did not initialize its child'
  cat targeted-sync.json
  exit 1
fi
if [ ! -f checkout/projects/missing/.git ]; then
  echo 'FAIL: initialized submodule did not use a Git file'
  exit 1
fi

# A real dirty neighbor must still block an all-module sync.
set +e
(cd checkout && /camp sync --dry-run --no-drain --json) > all-dry.json 2>&1
status=$?
set -e
if [ "$status" -eq 0 ]; then
  echo 'FAIL: dirty initialized child was not protected'
  cat all-dry.json
  exit 1
fi
if ! grep -q '"name": "projects/dirty"' all-dry.json || ! grep -q '"uncommittedChanges": true' all-dry.json; then
  echo 'FAIL: all-module dry run did not identify the dirty child itself'
  cat all-dry.json
  exit 1
fi
echo "PASS: empty child targeted sync and dry run succeeded; dirty neighbor rejected with exit $status"
