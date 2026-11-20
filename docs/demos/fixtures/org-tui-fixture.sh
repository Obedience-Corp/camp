#!/usr/bin/env bash
# Disposable registry for the camp org browser VHS tape.
#
# Nine orgs, thirty camps, written straight into a registry under a fake HOME.
# The org browser reads the registry only, so the camp directories need not
# exist and nothing here touches a real registry.
#
# Usage: eval "$(bash docs/demos/fixtures/org-tui-fixture.sh)"
set -euo pipefail

home_dir="$(mktemp -d /tmp/camp-org-tui-home.XXXXXX)"
registry="$home_dir/registry.json"

entry() {
    local id=$1 name=$2 org=$3 status=$4
    printf '    "%s": {"name":"%s","path":"%s/camps/%s","type":"product","org":"%s","status":"%s","last_access":"2026-06-16T10:00:00Z"}' \
        "$id" "$name" "$home_dir" "$name" "$org" "$status"
}

rows=(
    "atlas default active" "beacon default active" "compass default active"
    "harbor default active" "lantern default active" "meridian default active"
    "summit default active"
    "style-guide design active"
    "brand-kit marketing active" "launch-site marketing active"
    "newsletter marketing active" "events-archive marketing inactive"
    "press-archive marketing inactive"
    "build-cache devtools active" "lint-rules devtools active"
    "starter-camp examples active"
    "example-campaign festival-demo active" "festival-demo festival-demo active"
    "runbooks operations active" "vendor-audit operations inactive"
    "api-gateway web active" "checkout web active" "docs-site web active"
    "mobile-app web active" "search web active" "storefront web active"
    "web-console web active"
    "field-notes research active" "prototypes research active"
    "reading-list research active"
)

{
    printf '{\n  "version": 2,\n  "campaigns": {\n'
    n=0
    for row in "${rows[@]}"; do
        read -r name org status <<<"$row"
        n=$((n + 1))
        [[ $n -gt 1 ]] && printf ',\n'
        entry "$(printf 'ID-%02d' "$n")" "$name" "$org" "$status"
    done
    printf '\n  }\n}\n'
} >"$registry"

echo "export ORG_TUI_HOME=$home_dir"
echo "export HOME=$home_dir"
echo "export CAMP_REGISTRY_PATH=$registry"
echo "export CAMP_MACHINES_PATH=$home_dir/no-machines.yaml"
