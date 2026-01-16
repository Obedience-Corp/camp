#!/bin/bash
# Camp Project Management - Adding, listing, removing projects
# Projects are git repositories managed as submodules under projects/

echo "=== Project Management Examples ==="

# -----------------------------------------------------------------------------
# Add a new project from a remote repository
# -----------------------------------------------------------------------------
echo ""
echo "# Add a project from GitHub (HTTPS)"
echo "camp project add https://github.com/org/new-service.git"
echo "# Creates: projects/new-service/"
echo ""
echo "# Add a project from GitHub (SSH)"
echo "camp project add git@github.com:org/another-service.git"
echo "# Creates: projects/another-service/"

# Example output:
# Added project: new-service
#   Path:   projects/new-service
#   Source: https://github.com/org/new-service.git
#   Type:   go

# -----------------------------------------------------------------------------
# Add with a custom name
# -----------------------------------------------------------------------------
echo ""
echo "# Add with a custom name"
echo "camp project add git@github.com:org/long-repo-name.git --name short-name"
echo "# Creates: projects/short-name/"

# -----------------------------------------------------------------------------
# List all projects
# -----------------------------------------------------------------------------
echo ""
echo "# List projects (table format - default)"
echo "camp project list"
echo ""
echo "# Example output:"
echo "#   NAME          TYPE   PATH"
echo "#   api-service   go     projects/api-service"
echo "#   web-frontend  ts     projects/web-frontend"
echo "#   cli-tool      go     projects/cli-tool"

echo ""
echo "# List projects (JSON format for scripting)"
echo "camp project list --format json"

echo ""
echo "# List project names only"
echo "camp project list --format simple"

# -----------------------------------------------------------------------------
# Remove a project
# -----------------------------------------------------------------------------
echo ""
echo "# Remove project from tracking (keeps files)"
echo "camp project remove old-service"
echo "# Removes from git submodule, files remain in projects/old-service/"

echo ""
echo "# Remove project and delete files"
echo "camp project remove old-service --delete"
echo "# Requires confirmation unless --force is used"

echo ""
echo "# Dry run - see what would happen"
echo "camp project remove old-service --delete --dry-run"

# Example output:
# Dry run - would remove:
#   Project: old-service
#   - Remove from git submodule tracking
#   - Delete files at projects/old-service

# -----------------------------------------------------------------------------
# Scripting with projects
# -----------------------------------------------------------------------------
echo ""
echo "# Iterate over all projects"
echo "for project in \$(camp project list --format simple); do"
echo "    echo \"Processing \$project\""
echo "    # Get full path"
echo "    path=\$(camp go p \"\$project\" --print)"
echo "    # Do something with the project"
echo "    git -C \"\$path\" fetch --all"
echo "done"
