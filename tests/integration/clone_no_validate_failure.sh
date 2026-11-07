#!/bin/sh
# Run only inside a disposable container with /camp mounted read-only.
set -eu
export GIT_AUTHOR_NAME=Fixture GIT_AUTHOR_EMAIL=fixture@example.invalid
export GIT_COMMITTER_NAME=Fixture GIT_COMMITTER_EMAIL=fixture@example.invalid
git config --global init.defaultBranch main
mkdir -p /tmp/clone-no-validate-failure
cd /tmp/clone-no-validate-failure
git init -q source
mkdir -p source/.campaign
cat > source/.campaign/campaign.yaml <<'EOF'
id: 0592ff4e-e091-48bb-b32e-7aee45e018e9
name: invalid-clone-fixture
type: product
EOF
git -C source config -f .gitmodules submodule.ghost.path projects/ghost
git -C source config -f .gitmodules submodule.ghost.url /tmp/nonexistent-ghost-remote
git -C source add .
git -C source commit -qm 'declaration without gitlink'
set +e
/camp clone "$PWD/source" checkout --no-register --no-validate --json > clone.json 2>&1
status=$?
set -e
if [ "$status" -eq 0 ]; then
  echo 'FAIL: initialization failure disappeared with --no-validate'
  cat clone.json
  exit 1
fi
if ! grep -q 'projects/ghost' clone.json || ! grep -q '"success": false' clone.json; then
  echo 'FAIL: failed child or overall failure absent from JSON'
  cat clone.json
  exit 1
fi
echo "PASS: --no-validate retained clone initialization failure with exit $status"
