package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
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
	logCmd.GroupID = "git"
}

func runLog(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Suppress "signal: broken pipe" when pager quits early
	signal.Ignore(syscall.SIGPIPE)

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

	err = gitCmd.Run()
	if err != nil && !isSigpipeError(err) {
		return err
	}
	return nil
}

// isSigpipeError returns true if the error is caused by SIGPIPE (broken pipe).
// This occurs when the pager exits before git finishes writing output.
func isSigpipeError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EPIPE) {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.Signal() == syscall.SIGPIPE
		}
	}
	return false
}
