package main

import (
	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

var pushAllCmd = &cobra.Command{
	Use:   "all [git push flags]",
	Short: "Push all repos with unpushed commits",
	Long: `Push all repositories in the campaign that have unpushed commits.

Scans all submodules and the campaign root, checks which have commits
ahead of their upstream, and pushes them. Any extra flags are passed
through to git push for each repo.

Examples:
  camp push all              # Push all repos with unpushed commits
  camp push all --force      # Force push all repos
  camp push all -u origin    # Push and set upstream for all`,
	RunE:               runPushAllCmd,
	DisableFlagParsing: true,
}

func init() {
	pushCmd.AddCommand(pushAllCmd)
}

func runPushAllCmd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	return runPushAll(ctx, campRoot, args)
}
