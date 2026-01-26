package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [flags]",
	Short: "Show git status of the campaign",
	Long: `Show git status of the campaign root directory.

Works from anywhere within the campaign - always shows the status
of the campaign root repository.

Examples:
  camp status           # Full status
  camp status -s        # Short format
  camp status --short   # Short format`,
	RunE:               runStatus,
	DisableFlagParsing: true,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.GroupID = "campaign"
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	gitArgs := append([]string{"-C", campRoot, "status"}, args...)
	gitCmd := exec.CommandContext(ctx, "git", gitArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	gitCmd.Stdin = os.Stdin

	return gitCmd.Run()
}
