package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/triage"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/drain"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/notice"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var (
	statusSub      bool
	statusProject  string
	statusShort    bool
	statusShowRefs bool
	statusNoDrain  bool
)

var statusCmd = &cobra.Command{
	Use:   "status [flags] [-- <git-flags>]",
	Short: "Show git status of the camp",
	Long: `Show git status of the camp root directory.

Works from anywhere within the camp - always shows the status
of the camp root repository.

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
	statusCmd.Flags().BoolVar(&statusShowRefs, "show-refs", false, "Show camp root submodule ref changes")
	statusCmd.Flags().BoolVar(&statusNoDrain, "no-drain", false, "Do not report camp's queued commits first")
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp")
	}

	notice.Render(os.Stderr, notice.FilterDismissed(campRoot, notice.Detect(ctx, campRoot,
		notice.DungeonLegacy,
		notice.StaleLinks,
		notice.ArtifactRootNeverSynced,
		notice.ArtifactRootsMissingLocally,
		notice.ArtifactRootDrift,
	)))

	gitArgs, showRefsArg := extractShowRefs(args)
	showRefs := statusShowRefs || showRefsArg
	if statusShort {
		gitArgs = append(gitArgs, "--short")
	}

	target, err := git.ResolveTarget(ctx, campRoot, statusSub, statusProject)
	if err != nil {
		return camperrors.Wrap(err, "failed to resolve target")
	}

	if kind := target.NestedKindTitle(); kind != "" {
		fmt.Fprintln(os.Stderr, ui.Info(fmt.Sprintf("%s: %s", kind, target.Name)))
	}

	// Status reports; it does not wait. It is the first thing a user runs to
	// ask "did that commit?", and the honest answer to that while the queue is
	// still working is the count, given now, not the same answer given thirty
	// seconds later. A tree camp is midway through changing still has to be
	// declared, which is what the notice does.
	if !statusNoDrain {
		if _, err := drain.Note(ctx, target.Path); err != nil {
			return err
		}
	}

	// Hide submodule ref noise by default (only at campaign root)
	if !showRefs && !target.IsNestedRepo() {
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

	// One line when the campaign's last triage has gone stale. It reads the
	// verdict a refresh already cached and the campaign's own threshold —
	// never the filesystem at large — because this is the command people run
	// constantly and a banner that cost a discovery walk would be a tax on
	// every invocation.
	if !target.IsNestedRepo() {
		if err := triage.WriteBanner(ctx, os.Stdout, target.Path, time.Now()); err != nil {
			return camperrors.Wrap(err, "write triage notice")
		}
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
