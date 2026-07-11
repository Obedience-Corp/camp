package project

import (
	"io"

	projectlinked "github.com/Obedience-Corp/camp/cmd/camp/project/linked"
	projectremote "github.com/Obedience-Corp/camp/cmd/camp/project/remote"
	projectworktree "github.com/Obedience-Corp/camp/cmd/camp/project/worktree"
	"github.com/spf13/cobra"
)

// Cmd is the scaffold root for the project command family.
var Cmd = &cobra.Command{
	Use:     "project",
	Short:   "Manage campaign projects",
	GroupID: "project",
	Long: `Manage git submodules and project repositories in the campaign.

A project can be:
  - a git repository tracked as a submodule under projects/
  - a machine-local linked workspace attached via symlink under projects/

Use 'camp project add' for submodules and 'camp project link' / 'camp project unlink'
for linked workspaces. Use 'camp project run' (or the 'cr -p' shell shorthand)
to run a command inside a project from anywhere in the campaign.

Examples:
  camp project list                    List all projects
  camp project add git@github.com:org/repo.git  Add a new project
  camp project link ~/code/my-project  Link an existing local workspace
  camp project run -p fest -- just build  Run a command inside a project
  camp project commit -p fest -m "fix"  Commit changes in a project submodule
  camp project prune                   Delete merged branches in the cwd's project
  camp project worktree add my-branch --project fest  Create a worktree for a project
  camp project remove api-service      Remove a project`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	linkResolverFactory := func(stderr io.Writer, usageLine string) projectlinked.CampaignResolver {
		return newProjectCampaignResolver(stderr, usageLine)
	}
	Cmd.AddCommand(projectlinked.NewLinkCommand(linkResolverFactory))
	Cmd.AddCommand(projectlinked.NewUnlinkCommand(linkResolverFactory))
	Cmd.AddCommand(projectremote.Cmd)
	Cmd.AddCommand(projectworktree.Cmd)
}
