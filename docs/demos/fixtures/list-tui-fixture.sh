#!/usr/bin/env bash
# Disposable registry and camp directories for the camp list browser VHS tape.
#
# Nine orgs with thirty-three camps plus one empty org, under a fake HOME.
# Each camp gets the campaign.yaml that registry verification reads, so the
# browser opens without a verification notice. Nothing here touches a real
# registry, and no machines file exists, so nothing reaches the network.
#
# Usage: eval "$(bash docs/demos/fixtures/list-tui-fixture.sh)"
set -euo pipefail

home_dir="$(mktemp -d /tmp/camp-list-tui-home.XXXXXX)"
registry="$home_dir/registry.json"

rows=(
    "dotfiles default active" "inbox default inactive" "scratchpad default active"
    "brand-kit design active" "icon-set design active" "style-guide design active"
    "type-specimens design inactive"
    "build-cache devtools active" "ci-runners devtools active"
    "lint-rules devtools active" "release-tooling devtools inactive"
    "test-harness devtools active"
    "example-go-service examples active" "example-web-app examples active"
    "demo-marketplace festival-demo active" "demo-onboarding festival-demo active"
    "demo-recording-kit festival-demo inactive" "demo-social-fitness festival-demo active"
    "launch-site marketing active" "newsletter marketing active"
    "press-archive marketing inactive"
    "incident-reviews operations active" "runbooks operations active"
    "vendor-audit operations inactive"
    "field-notes research active" "prototypes research active"
    "reading-list research inactive"
    "api-gateway web active" "checkout web active"
    "customer-portal-and-billing web active" "docs-site web inactive"
    "mobile-app web active" "storefront web active"
)

{
    printf '{\n  "version": 2,\n  "orgs": [{"name": "archive"}],\n  "campaigns": {\n'
    n=0
    for row in "${rows[@]}"; do
        read -r name org status <<<"$row"
        n=$((n + 1))
        id="$(printf '%02daa0000-0000-4000-8000-0000000000%02d' "$n" "$n")"
        path="$home_dir/work/$org/$name"
        mkdir -p "$path/.campaign"
        printf 'id: %s\nname: %s\ntype: product\n' "$id" "$name" >"$path/.campaign/campaign.yaml"
        [[ $n -gt 1 ]] && printf ',\n'
        printf '    "%s": {"name":"%s","path":"%s","type":"product","org":"%s","status":"%s","last_access":"2026-06-16T10:00:00Z"}' \
            "$id" "$name" "$path" "$org" "$status"
    done
    printf '\n  }\n}\n'
} >"$registry"

echo "export LIST_TUI_HOME=$home_dir"
echo "export HOME=$home_dir"
echo "export CAMP_REGISTRY_PATH=$registry"
echo "export CAMP_MACHINES_PATH=$home_dir/no-machines.yaml"
