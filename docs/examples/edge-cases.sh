#!/bin/bash
# Camp Edge Cases - Error handling and special scenarios
# Understanding how camp behaves in unusual situations

echo "=== Edge Cases and Error Handling ==="

# -----------------------------------------------------------------------------
# Not in a campaign
# -----------------------------------------------------------------------------
echo ""
echo "# When not in a campaign directory"
echo "cd /tmp"
echo "cgo p"
echo "# Error: not inside a campaign directory"
echo "# Hint: Run 'camp init' to create one, or navigate to an existing campaign"

echo ""
echo "# The same applies to camp commands"
echo "camp project list"
echo "# Error: not inside a campaign directory"

# -----------------------------------------------------------------------------
# No matches found
# -----------------------------------------------------------------------------
echo ""
echo "# When fuzzy search finds nothing"
echo "cgo p nonexistent"
echo "# Error: no matches found for 'nonexistent' in projects"

echo ""
echo "# This also applies to categories"
echo "cgo xyz"
echo "# Error: unknown category or no matches: xyz"

# -----------------------------------------------------------------------------
# Multiple matches
# -----------------------------------------------------------------------------
echo ""
echo "# When multiple matches exist"
echo "cgo p api"
echo "# If api-service, api-gateway, api-common all exist:"
echo "# Multiple matches found:"
echo "#   api-service"
echo "#   api-gateway"
echo "#   api-common"
echo "# Using best match: api-service"
echo ""
echo "# Tip: Be more specific to get exact match"
echo "cgo p api-service    # Exact match, no ambiguity"

# -----------------------------------------------------------------------------
# Empty categories
# -----------------------------------------------------------------------------
echo ""
echo "# When a category directory is empty"
echo "cgo p"
echo "# Navigates to projects/ but lists nothing"

echo ""
echo "# When category directory doesn't exist"
echo "# (rare, since camp init creates all directories)"
echo "rmdir festivals"
echo "cgo f"
echo "# Error: category directory does not exist: festivals"

# -----------------------------------------------------------------------------
# Worktree navigation
# -----------------------------------------------------------------------------
echo ""
echo "# Worktree navigation uses @ syntax"
echo "cgo wt api-service@"
echo "# Lists branches for api-service worktree"
echo ""
echo "cgo wt api-service@feature"
echo "# Fuzzy matches to api-service@feature-branch"

# -----------------------------------------------------------------------------
# Campaign registration conflicts
# -----------------------------------------------------------------------------
echo ""
echo "# Registering a campaign with the same name"
echo "camp register --name existing-name"
echo "# Warning: Campaign 'existing-name' already registered at /some/path"
echo "# Replace with new path? [y/N]"

echo ""
echo "# Initializing in an existing campaign"
echo "cd ~/existing-campaign"
echo "camp init"
echo "# Skips existing directories, only creates missing ones"

# -----------------------------------------------------------------------------
# Invalid arguments
# -----------------------------------------------------------------------------
echo ""
echo "# Invalid campaign type"
echo "camp init --type invalid"
echo "# Error: invalid campaign type: invalid (must be product, research, tools, or personal)"

echo ""
echo "# Missing required argument"
echo "camp project remove"
echo "# Error: accepts 1 arg(s), received 0"

# -----------------------------------------------------------------------------
# Context timeout
# -----------------------------------------------------------------------------
echo ""
echo "# Shell completion has a 50ms timeout"
echo "# If the campaign is on a slow filesystem, completion may be limited"
echo "# This is by design to keep the shell responsive"

# -----------------------------------------------------------------------------
# Shell integration issues
# -----------------------------------------------------------------------------
echo ""
echo "# If cgo isn't working"
echo "echo \$SHELL             # Check your shell"
echo "camp shell-init zsh    # Generate init script"
echo "# Make sure eval is in your shell config and source it"

echo ""
echo "# Check if shell function is defined"
echo "type cgo              # Should show 'cgo is a shell function'"
