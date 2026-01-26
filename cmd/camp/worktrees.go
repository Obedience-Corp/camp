package main

import (
	"github.com/spf13/cobra"
)

var worktreesCmd = &cobra.Command{
	Use:        "worktrees",
	Short:      "Manage git worktrees for projects",
	Deprecated: "use 'camp project worktree' instead for project-scoped operations",
	Long: `Manage git worktrees across campaign projects.

Worktrees allow you to have multiple working directories for the same repository,
enabling parallel development on different branches without stashing or switching.

All worktrees are created in a centralized location:
  projects/worktrees/<project>/<worktree-name>/

Commands:
  create    Create a new worktree for a project
  list      List all worktrees
  info      Show information about a worktree
  commit    Commit changes in a worktree
  clean     Remove stale worktrees

Examples:
  # Create a worktree for feature development
  camp worktrees create my-api feature-auth

  # Create a worktree with a new branch
  camp worktrees create my-api experiment --new-branch

  # List all worktrees
  camp worktrees list

  # List worktrees for a specific project
  camp worktrees list --project my-api

  # Show current worktree info (when inside one)
  camp worktrees info

  # Commit changes from within a worktree
  camp worktrees commit -m "Add feature"

  # Clean up stale worktrees
  camp worktrees clean --all

Use "camp worktrees [command] --help" for more information about a command.`,
	Aliases: []string{"wt"},
}

func init() {
	rootCmd.AddCommand(worktreesCmd)
	worktreesCmd.GroupID = "project"
}
