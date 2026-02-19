# Release and versioning recipes

# Show available release commands (default)
[private]
default:
    @just --list --unsorted --justfile {{source_file()}}

# Create next release and push (patch|minor|major)
[no-cd]
release level="patch":
    #!/usr/bin/env bash
    set -euo pipefail
    suffix="alpha-stable"
    level="{{level}}"

    latest=$(git tag -l "v*-${suffix}" --sort=-version:refname | head -1)

    if [ -z "$latest" ]; then
        new="v0.1.0-${suffix}"
    else
        nums="${latest#v}"
        nums="${nums%-${suffix}}"
        IFS='.' read -r major minor patch <<< "$nums"

        case "$level" in
            major) major=$((major + 1)); minor=0; patch=0 ;;
            minor) minor=$((minor + 1)); patch=0 ;;
            patch) patch=$((patch + 1)) ;;
            *) echo "Unknown level: $level (use patch, minor, or major)"; exit 1 ;;
        esac
        new="v${major}.${minor}.${patch}-${suffix}"
    fi

    git tag -a "$new" -m "$new"
    git push origin "$new"
    echo "Released $new"

# Show current version tag
[no-cd]
current:
    #!/usr/bin/env bash
    set -euo pipefail
    latest=$(git tag -l "v*-alpha-stable" --sort=-version:refname | head -1)
    if [ -z "$latest" ]; then
        echo "No releases yet"
    else
        echo "$latest"
    fi

# List all release tags
[no-cd]
tags:
    @git tag -l "v*-alpha-stable" --sort=-version:refname
