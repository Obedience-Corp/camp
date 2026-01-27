package main

import (
	"github.com/spf13/cobra"
)

var projectWorktreeCmd = &cobra.Command{
	Use:     "worktree",
	Short:   "Manage worktrees for a project",
	Aliases: []string{"wt"},
	Long: `Manage git worktrees for the current project.

Worktrees allow you to have multiple working directories for the same repository,
enabling parallel development on different branches without stashing or switching.

Auto-detects the current project from your working directory, or use --project
to specify explicitly.

All worktrees are created at: projects/worktrees/<project>/<worktree-name>/

Commands:
  add       Create a new worktree
  list      List worktrees for the project
  remove    Remove a worktree

Examples:
  # From within a project directory
  cd projects/my-api
  camp project worktree add feature-auth      # Creates new branch based on current
  camp project worktree add fix --start-point main  # New branch based on main
  camp project worktree list
  camp project worktree remove feature-auth

  # With explicit project
  camp project worktree add feature-xyz --project my-api`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	projectCmd.AddCommand(projectWorktreeCmd)
}
