package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log [flags]",
	Short: "Show git log of the campaign",
	Long: `Show git log of the campaign root repository.

Works from anywhere within the campaign - always shows the log
of the campaign root repository.

Examples:
  camp log              # Full log
  camp log -5           # Last 5 commits
  camp log --oneline    # One line per commit
  camp log --graph      # Show branch graph`,
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

	gitArgs := append([]string{"-C", campRoot, "log"}, args...)
	gitCmd := exec.CommandContext(ctx, "git", gitArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	gitCmd.Stdin = os.Stdin

	return gitCmd.Run()
}
