#!/bin/bash
# Camp Quick Start - Get navigating in 60 seconds
# Run: bash quick-start.sh

set -e  # Exit on error

echo "=== Camp Quick Start ==="

# Step 1: Install camp
echo ""
echo "Step 1: Install camp"
echo "  go install github.com/Obedience-Corp/camp@latest"

# Step 2: Create your first campaign
echo ""
echo "Step 2: Create a campaign"
echo "  mkdir my-workspace && cd my-workspace"
echo "  camp init"

# Example output:
# Campaign initialized successfully!
#
# Directories created:
#   .campaign/
#   projects/
#   festivals/
#   ai_docs/
#   docs/
#   corpus/
#   worktrees/
#   pipelines/
#   code_reviews/
#
# Files created:
#   .campaign/campaign.yaml
#   CLAUDE.md
#   AGENTS.md

# Step 3: Add shell integration
echo ""
echo "Step 3: Add shell integration (one-time setup)"
echo "  # For zsh:"
echo "  echo 'eval \"\$(camp shell-init zsh)\"' >> ~/.zshrc"
echo "  source ~/.zshrc"
echo ""
echo "  # For bash:"
echo "  echo 'eval \"\$(camp shell-init bash)\"' >> ~/.bashrc"
echo "  source ~/.bashrc"
echo ""
echo "  # For fish:"
echo "  echo 'camp shell-init fish | source' >> ~/.config/fish/config.fish"
echo "  source ~/.config/fish/config.fish"

# Step 4: Add some projects
echo ""
echo "Step 4: Add projects"
echo "  camp project add https://github.com/user/api-service.git"
echo "  camp project add https://github.com/user/web-app.git"

# Step 5: Navigate!
echo ""
echo "Step 5: Navigate with cgo!"
echo "  cgo              # Jump to campaign root"
echo "  cgo p            # Jump to projects/"
echo "  cgo p api        # Fuzzy find 'api' in projects/"
echo "  cgo f            # Jump to festivals/"

echo ""
echo "=== Category Shortcuts ==="
echo "  p  = projects/       c  = corpus/        f  = festivals/"
echo "  a  = ai_docs/        d  = docs/          w  = worktrees/"
echo "  r  = code_reviews/   pi = pipelines/"

echo ""
echo "You're ready to go! Run 'cgo --help' for more options."
