package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push [flags] [remote] [branch]",
	Short: "Push campaign changes to remote",
	Long: `Push campaign changes to the remote repository.

Works from anywhere within the campaign - always pushes from
the campaign root repository.

Examples:
  camp push                    # Push current branch
  camp push origin main        # Push to specific remote/branch
  camp push --force            # Force push
  camp push -u origin feature  # Push and set upstream`,
	RunE:               runPush,
	DisableFlagParsing: true,
}

func init() {
	rootCmd.AddCommand(pushCmd)
	pushCmd.GroupID = "campaign"
}

func runPush(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	gitArgs := append([]string{"-C", campRoot, "push"}, args...)
	gitCmd := exec.CommandContext(ctx, "git", gitArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	gitCmd.Stdin = os.Stdin

	return gitCmd.Run()
}
