package main

import (
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"
	"os/exec"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var (
	statusSub      bool
	statusProject  string
	statusShort    bool
	statusShowRefs bool
)

var statusCmd = &cobra.Command{
	Use:   "status [flags] [-- <git-flags>]",
	Short: "Show git status of the campaign",
	Long: `Show git status of the campaign root directory.

Works from anywhere within the campaign - always shows the status
of the campaign root repository.

Use --sub to show status of the submodule detected from your current directory.
Use --project/-p to show status of a specific project.
Pass git status flags after -- to forward them directly to git.`,
	Example: `  camp status           # Full status
  camp status -s        # Short format
  camp status --sub     # Status of current submodule
  camp status -p projects/camp  # Status of camp project`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	statusCmd.GroupID = "git"
	statusCmd.Flags().BoolVar(&statusSub, "sub", false, "Status of the submodule detected from current directory")
	statusCmd.Flags().StringVarP(&statusProject, "project", "p", "", "Status of a specific project path")
	statusCmd.Flags().BoolVarP(&statusShort, "short", "s", false, "Give output in short format")
	statusCmd.Flags().BoolVar(&statusShowRefs, "show-refs", false, "Show campaign root submodule ref changes")
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	gitArgs, showRefsArg := extractShowRefs(args)
	showRefs := statusShowRefs || showRefsArg
	if statusShort {
		gitArgs = append(gitArgs, "--short")
	}

	target, err := git.ResolveTarget(ctx, campRoot, statusSub, statusProject)
	if err != nil {
		return camperrors.Wrap(err, "failed to resolve target")
	}

	if target.IsSubmodule {
		fmt.Fprintln(os.Stderr, ui.Info(fmt.Sprintf("Submodule: %s", target.Name)))
	}

	// Hide submodule ref noise by default (only at campaign root)
	if !showRefs && !target.IsSubmodule {
		gitArgs = append(gitArgs, "--ignore-submodules=all")
	}

	fullArgs := append([]string{"-C", target.Path, "status"}, gitArgs...)
	gitCmd := exec.CommandContext(ctx, "git", fullArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	gitCmd.Stdin = os.Stdin

	if err := gitCmd.Run(); err != nil {
		return camperrors.Wrapf(err, "git status failed for %s", target.Path)
	}
	return nil
}

// extractShowRefs removes --show-refs from args and returns the filtered
// args plus a boolean indicating whether the flag was present.
func extractShowRefs(args []string) ([]string, bool) {
	filtered := make([]string, 0, len(args))
	found := false
	for _, arg := range args {
		if arg == "--show-refs" {
			found = true
		} else {
			filtered = append(filtered, arg)
		}
	}
	return filtered, found
}
