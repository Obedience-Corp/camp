//go:build dev

package main

import (
	"github.com/spf13/cobra"
)

// flagRemoteProject is the shared --project flag for all remote subcommands.
var flagRemoteProject string

var projectRemoteCmd = &cobra.Command{
	Use:     "remote",
	Short:   "Manage remotes for a project",
	Aliases: []string{"rem"},
	Long: `Manage git remotes for a campaign project.

Auto-detects the current project from your working directory, or use --project
to specify explicitly.

Commands:
  list      List remotes (default)
  set-url   Update a remote URL atomically across all locations
  add       Add a new remote
  remove    Remove a remote
  rename    Rename a remote

Examples:
  # From within a project directory
  cd projects/my-api
  camp project remote                          # List remotes
  camp project remote set-url git@github.com:org/new-repo.git
  camp project remote add upstream git@github.com:org/upstream.git
  camp project remote remove upstream
  camp project remote rename upstream fork

  # With explicit project
  camp project remote list --project my-api`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProjectRemoteList(cmd, args)
	},
}

func init() {
	projectCmd.AddCommand(projectRemoteCmd)

	projectRemoteCmd.PersistentFlags().StringVarP(&flagRemoteProject, "project", "p", "",
		"Project name (auto-detected from cwd if not specified)")

	projectRemoteCmd.RegisterFlagCompletionFunc("project", completeProjectName)
}
