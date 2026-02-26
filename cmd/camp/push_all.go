package main

import (
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

var pushAllNoRecurse bool

var pushAllCmd = &cobra.Command{
	Use:   "all [git push flags]",
	Short: "Push all repos with unpushed commits",
	Long: `Push all repositories in the campaign that have unpushed commits.

Scans all submodules and the campaign root, checks which have commits
ahead of their upstream, and pushes them. Any extra flags are passed
through to git push for each repo.

By default, nested submodules (e.g. inside monorepos) are included.
Use --no-recurse to only push top-level submodules.

Examples:
  camp push all              # Push all repos with unpushed commits
  camp push all --force      # Force push all repos
  camp push all -u origin    # Push and set upstream for all
  camp push all --no-recurse # Only top-level submodules`,
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

	// Extract --no-recurse before passing remaining args to git.
	var noRecurse bool
	args, noRecurse = extractNoRecurse(args)

	return runPushAll(ctx, campRoot, args, noRecurse)
}

// extractNoRecurse removes --no-recurse from args and returns the filtered
// args plus whether the flag was present. Used by commands with
// DisableFlagParsing that need to intercept camp-specific flags.
func extractNoRecurse(args []string) ([]string, bool) {
	var filtered []string
	var found bool
	for _, a := range args {
		if a == "--no-recurse" {
			found = true
			continue
		}
		filtered = append(filtered, a)
	}
	return filtered, found
}
