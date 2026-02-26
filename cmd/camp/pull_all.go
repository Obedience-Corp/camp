package main

import (
	"github.com/Obedience-Corp/camp/internal/campaign"
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

By default, nested submodules (e.g. inside monorepos) are included.
Use --no-recurse to only pull top-level submodules.

Examples:
  camp pull all              # Pull all repos
  camp pull all --rebase     # Pull all repos with rebase
  camp pull all --ff-only    # Fast-forward only for all repos
  camp pull all --no-recurse # Only top-level submodules`,
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

	// Extract --no-recurse before passing remaining args to git.
	var noRecurse bool
	args, noRecurse = extractNoRecurse(args)

	return runPullAll(ctx, campRoot, args, noRecurse)
}
