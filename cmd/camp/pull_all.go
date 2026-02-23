package main

import (
	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

var pullAllCmd = &cobra.Command{
	Use:   "all [git pull flags]",
	Short: "Pull latest changes for all repos",
	Long: `Pull latest changes for all repositories in the campaign.

Scans the campaign root and all submodules, checks which have a tracking
branch with upstream, and pulls them. Any extra flags are passed through
to git pull for each repo.

Repos in detached HEAD state or without upstream tracking are skipped.

Examples:
  camp pull all              # Pull all repos
  camp pull all --rebase     # Pull all repos with rebase
  camp pull all --ff-only    # Fast-forward only for all repos`,
	RunE:               runPullAllCmd,
	DisableFlagParsing: true,
}

func init() {
	pullCmd.AddCommand(pullAllCmd)
}

func runPullAllCmd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	return runPullAll(ctx, campRoot, args)
}
