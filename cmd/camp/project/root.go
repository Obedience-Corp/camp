package project

import (
	projectworktree "github.com/Obedience-Corp/camp/cmd/camp/project/worktree"
	"github.com/spf13/cobra"
)

// Cmd is the scaffold root for the project command family.
var Cmd = &cobra.Command{
	Use:     "project",
	Short:   "Manage campaign projects",
	GroupID: "project",
	Long: `Manage git submodules and project repositories in the campaign.

A project is a git repository tracked as a submodule under the projects/ directory.
Projects can be added from remote URLs or existing local repositories.

Examples:
  camp project list                    List all projects
  camp project add git@github.com:org/repo.git  Add a new project
  camp project remove api-service      Remove a project`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	Cmd.AddCommand(projectworktree.Cmd)
}
