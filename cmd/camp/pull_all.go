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
Use --default-branch to auto-checkout each submodule's default branch
before pulling. This is useful when submodules are on stale feature
branches whose remote tracking branch has been deleted.

By default, nested submodules (e.g. inside monorepos) are included.
Use --no-recurse to only pull top-level submodules.

Examples:
  camp pull all                      # Pull all repos
  camp pull all --rebase             # Pull all repos with rebase
  camp pull all --ff-only            # Fast-forward only for all repos
  camp pull all --no-recurse         # Only top-level submodules
  camp pull all --default-branch     # Checkout default branch first`,
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

	// Extract camp-specific flags before passing remaining args to git.
	var noRecurse bool
	args, noRecurse = extractNoRecurse(args)

	var useDefault bool
	args, useDefault = extractDefaultBranch(args)

	return runPullAll(ctx, campRoot, args, noRecurse, useDefault)
}

// extractDefaultBranch removes --default-branch from args and returns the
// filtered args plus whether the flag was present.
func extractDefaultBranch(args []string) ([]string, bool) {
	var filtered []string
	var found bool
	for _, a := range args {
		if a == "--default-branch" {
			found = true
			continue
		}
		filtered = append(filtered, a)
	}
	return filtered, found
}
