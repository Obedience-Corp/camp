#!/usr/bin/env bash
# Disposable three-camp registry for the switch-selector VHS tape.
#
# last_access oldest → newest = alpha, beta, gamma. No machines, so the
# picker is local-only and the geometry lock is visible.
#
# Usage: eval "$(CAMP_BIN=$PWD/bin/camp bash docs/demos/fixtures/switch-selector-fixture.sh)"
set -euo pipefail

home_dir="$(mktemp -d /tmp/camp-switch-selector-home.XXXXXX)"
mkdir -p "$home_dir/camps"/{alpha,beta,gamma}
registry="$home_dir/registry.json"

cat >"$registry" <<EOF
{
  "version": 2,
  "campaigns": {
    "id-alpha": {
      "name": "alpha",
      "path": "$home_dir/camps/alpha",
      "type": "product",
      "status": "active",
      "last_access": "2026-01-01T00:00:00Z"
    },
    "id-beta": {
      "name": "beta",
      "path": "$home_dir/camps/beta",
      "type": "product",
      "status": "active",
      "last_access": "2026-06-01T00:00:00Z"
    },
    "id-gamma": {
      "name": "gamma",
      "path": "$home_dir/camps/gamma",
      "type": "product",
      "status": "active",
      "last_access": "2026-09-01T00:00:00Z"
    }
  }
}
EOF

echo "export SWITCH_SELECTOR_HOME=$home_dir"
echo "export SWITCH_SELECTOR_REGISTRY=$registry"
echo "export HOME=$home_dir"
echo "export CAMP_REGISTRY_PATH=$registry"
