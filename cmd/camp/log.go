package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/git"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log [flags]",
	Short: "Show git log of the campaign",
	Long: `Show git log of the campaign root repository.

Works from anywhere within the campaign - always shows the log
of the campaign root repository.

Use --sub to show log of the submodule detected from your current directory.
Use --project/-p to show log of a specific project.

Examples:
  camp log              # Full log
  camp log -5           # Last 5 commits
  camp log --oneline    # One line per commit
  camp log --graph      # Show branch graph
  camp log --sub        # Log of current submodule
  camp log -p projects/camp --oneline  # Log of camp project`,
	RunE:               runLog,
	DisableFlagParsing: true,
}

func init() {
	rootCmd.AddCommand(logCmd)
	logCmd.GroupID = "campaign"
}

func runLog(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	// Extract camp-specific flags, pass rest to git
	gitArgs, sub, project := git.ExtractSubFlags(args)

	target, err := git.ResolveTarget(ctx, campRoot, sub, project)
	if err != nil {
		return fmt.Errorf("failed to resolve target: %w", err)
	}

	if target.IsSubmodule {
		fmt.Fprintln(os.Stderr, ui.Info(fmt.Sprintf("Submodule: %s", target.Name)))
	}

	fullArgs := append([]string{"-C", target.Path, "log"}, gitArgs...)
	gitCmd := exec.CommandContext(ctx, "git", fullArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	gitCmd.Stdin = os.Stdin

	return gitCmd.Run()
}
