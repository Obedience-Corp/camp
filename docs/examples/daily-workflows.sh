#!/bin/bash
# Camp Daily Workflows - Common navigation patterns
# These examples assume shell integration is set up (see quick-start.sh)

echo "=== Daily Workflow Examples ==="

# -----------------------------------------------------------------------------
# Morning routine - check campaign status
# -----------------------------------------------------------------------------
echo ""
echo "# Morning routine"
echo "cgo                    # Jump to campaign root"
echo "camp list              # See all registered campaigns"
echo "camp project list      # See projects in current campaign"

# Example output from 'camp project list':
# NAME          TYPE   PATH
# api-service   go     projects/api-service
# web-app       ts     projects/web-app
# cli-tool      go     projects/cli-tool

# -----------------------------------------------------------------------------
# Navigate to work on a project
# -----------------------------------------------------------------------------
echo ""
echo "# Navigate to a project"
echo "cgo p api              # Fuzzy find 'api' -> api-service"
echo "git pull               # Update the project"
echo "# ... do your work ..."

# -----------------------------------------------------------------------------
# Quick directory jumps
# -----------------------------------------------------------------------------
echo ""
echo "# Quick directory jumps"
echo "cgo d                  # docs/ directory"
echo "cgo a                  # ai_docs/ directory"
echo "cgo c                  # corpus/ directory"
echo "cgo w                  # worktrees/ directory"

# -----------------------------------------------------------------------------
# Work with festivals
# -----------------------------------------------------------------------------
echo ""
echo "# Festival workflow"
echo "cgo f                  # festivals/ directory"
echo "cgo f active           # festivals/active/ (if exists)"
echo "fest status            # Check festival progress (if fest installed)"

# -----------------------------------------------------------------------------
# Return to previous work
# -----------------------------------------------------------------------------
echo ""
echo "# Return to a project"
echo "cgo p api              # Fuzzy match jumps back to api-service"

# -----------------------------------------------------------------------------
# Run commands without changing directory
# -----------------------------------------------------------------------------
echo ""
echo "# Run commands without cd (using -c flag)"
echo "cgo p -c ls            # List projects/ contents"
echo "cgo f -c ls -la        # List festivals/ with details"
echo "cgo p api -c git status  # Git status in api-service"

# Example: running git status in each project
echo ""
echo "# Example: check git status in all projects"
echo "for proj in \$(camp project list --format simple); do"
echo "    echo \"=== \$proj ===\""
echo "    cgo p \"\$proj\" -c git status --short"
echo "done"
