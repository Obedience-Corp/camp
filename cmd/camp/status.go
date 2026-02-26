package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [flags]",
	Short: "Show git status of the campaign",
	Long: `Show git status of the campaign root directory.

Works from anywhere within the campaign - always shows the status
of the campaign root repository.

Use --sub to show status of the submodule detected from your current directory.
Use --project/-p to show status of a specific project.

Examples:
  camp status           # Full status
  camp status -s        # Short format (git flag)
  camp status --short   # Short format (git flag)
  camp status --sub     # Status of current submodule
  camp status -p projects/camp  # Status of camp project`,
	RunE:               runStatus,
	DisableFlagParsing: true,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.GroupID = "git"
}

func runStatus(cmd *cobra.Command, args []string) error {
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

	fullArgs := append([]string{"-C", target.Path, "status"}, gitArgs...)
	gitCmd := exec.CommandContext(ctx, "git", fullArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	gitCmd.Stdin = os.Stdin

	return gitCmd.Run()
}
