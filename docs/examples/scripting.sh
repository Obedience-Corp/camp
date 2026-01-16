#!/bin/bash
# Camp Scripting Examples - Using camp in shell scripts
# The --print flag and JSON output make camp scriptable

echo "=== Scripting with Camp ==="

# -----------------------------------------------------------------------------
# Get paths programmatically with --print
# -----------------------------------------------------------------------------
echo ""
echo "# Get a path without changing directory"
echo "PROJECT_PATH=\$(camp go p api-service --print)"
echo "echo \"Project is at: \$PROJECT_PATH\""
echo ""
echo "# Get category paths"
echo "PROJECTS_DIR=\$(camp go p --print)"
echo "FESTIVALS_DIR=\$(camp go f --print)"
echo "CORPUS_DIR=\$(camp go c --print)"

# -----------------------------------------------------------------------------
# Run commands from directories without cd
# -----------------------------------------------------------------------------
echo ""
echo "# Run command in a directory (using -c flag)"
echo "cgo p -c ls                    # List projects/"
echo "cgo f -c ls -la               # List festivals/ with details"
echo "cgo p api -c git status       # Git status in api-service"
echo "cgo p api -c make test        # Run tests in api-service"

# -----------------------------------------------------------------------------
# Batch operations on all projects
# -----------------------------------------------------------------------------
echo ""
echo "# Pull all projects"
echo "for project in \$(camp project list --format simple); do"
echo "    echo \"Pulling \$project...\""
echo "    cgo p \"\$project\" -c git pull"
echo "done"

echo ""
echo "# Check status of all projects"
echo "for project in \$(camp project list --format simple); do"
echo "    echo \"=== \$project ===\""
echo "    cgo p \"\$project\" -c git status --short"
echo "done"

echo ""
echo "# Build all Go projects"
echo "for project in \$(camp project list --format simple); do"
echo "    path=\$(camp go p \"\$project\" --print)"
echo "    if [ -f \"\$path/go.mod\" ]; then"
echo "        echo \"Building \$project...\""
echo "        (cd \"\$path\" && go build ./...)"
echo "    fi"
echo "done"

# -----------------------------------------------------------------------------
# JSON output for advanced scripting
# -----------------------------------------------------------------------------
echo ""
echo "# Parse JSON with jq"
echo "camp list --format json | jq -r '.[].path'"
echo ""
echo "# Get all campaign names"
echo "camp list --format json | jq -r '.[].name'"
echo ""
echo "# Filter campaigns by type"
echo "camp list --format json | jq -r '.[] | select(.type == \"product\") | .name'"

echo ""
echo "# Project info as JSON"
echo "camp project list --format json | jq '.[0]'"

# -----------------------------------------------------------------------------
# Environment variables
# -----------------------------------------------------------------------------
echo ""
echo "# Set up environment based on campaign"
echo "export CAMP_ROOT=\$(camp go --print)"
echo "export CAMP_PROJECTS=\$(camp go p --print)"
echo "export CAMP_FESTIVALS=\$(camp go f --print)"

# -----------------------------------------------------------------------------
# Integrate with other tools
# -----------------------------------------------------------------------------
echo ""
echo "# Open project in VSCode"
echo "code \$(camp go p api-service --print)"

echo ""
echo "# Create a new file in a category"
echo "touch \$(camp go d --print)/new-doc.md"

echo ""
echo "# Copy files between categories"
echo "cp design.md \$(camp go d --print)/"

# -----------------------------------------------------------------------------
# Error handling in scripts
# -----------------------------------------------------------------------------
echo ""
echo "# Check if in a campaign"
echo "if camp go --print >/dev/null 2>&1; then"
echo "    echo \"In a campaign\""
echo "else"
echo "    echo \"Not in a campaign\""
echo "    exit 1"
echo "fi"

echo ""
echo "# Check if project exists"
echo "if camp go p my-project --print >/dev/null 2>&1; then"
echo "    echo \"Project exists\""
echo "else"
echo "    echo \"Project not found\""
echo "fi"
