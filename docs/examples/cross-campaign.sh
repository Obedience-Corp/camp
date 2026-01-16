#!/bin/bash
# Camp Cross-Campaign Navigation - Working with multiple campaigns
# Campaigns are registered in ~/.config/campaign/registry.yaml

echo "=== Cross-Campaign Navigation ==="

# -----------------------------------------------------------------------------
# Setting up multiple campaigns
# -----------------------------------------------------------------------------
echo ""
echo "# Initialize multiple campaigns"
echo "cd ~/work/project-alpha"
echo "camp init --name project-alpha --type product"
echo ""
echo "cd ~/work/project-beta"
echo "camp init --name project-beta --type research"

# Note: camp init automatically registers the campaign

# -----------------------------------------------------------------------------
# Registering existing campaigns
# -----------------------------------------------------------------------------
echo ""
echo "# Register an existing campaign that wasn't created with camp"
echo "cd ~/work/legacy-project"
echo "camp register"
echo ""
echo "# Register with a custom name"
echo "camp register --name my-custom-name"
echo ""
echo "# Register a specific path"
echo "camp register ~/work/some-project"

# -----------------------------------------------------------------------------
# List all campaigns
# -----------------------------------------------------------------------------
echo ""
echo "# List all registered campaigns"
echo "camp list"
echo ""
echo "# Example output:"
echo "#   NAME            TYPE      PATH"
echo "#   project-alpha   product   ~/work/project-alpha"
echo "#   project-beta    research  ~/work/project-beta"
echo "#   guild-framework product   ~/Dev/AI/guild-framework"

echo ""
echo "# List sorted by name"
echo "camp list --sort name"

echo ""
echo "# Output as JSON for scripting"
echo "camp list --format json"

echo ""
echo "# Get just the names"
echo "camp list --format simple"

# -----------------------------------------------------------------------------
# Navigate across campaigns
# -----------------------------------------------------------------------------
echo ""
echo "# Navigate to a different campaign"
echo "cgo project-beta       # Jump to project-beta root"
echo "cgo p                  # Now in project-beta's projects/"
echo ""
echo "# Navigate to specific project in that campaign"
echo "cgo project-beta p api  # Jump to project-beta's api project"

# -----------------------------------------------------------------------------
# Campaign management
# -----------------------------------------------------------------------------
echo ""
echo "# Unregister a campaign (does NOT delete files)"
echo "camp unregister project-beta"
echo ""
echo "# Force unregister without confirmation"
echo "camp unregister project-beta --force"

echo ""
echo "# Re-register after moving"
echo "mv ~/work/project-beta ~/dev/project-beta"
echo "camp unregister project-beta"
echo "cd ~/dev/project-beta"
echo "camp register"

# -----------------------------------------------------------------------------
# Registry location
# -----------------------------------------------------------------------------
echo ""
echo "# Registry is stored at:"
echo "# ~/.config/campaign/registry.yaml"
