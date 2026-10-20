package main

import (
	"context"
	"io"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/defercommit"
	"github.com/Obedience-Corp/camp/internal/git"
)

// queueBeforeStage returns the artifact roots declared before this command
// and whether --auto-write queued the commit without the foreground stage.
//
// A root created while handling the guard is reported there; listing it
// again would say the exclusion twice. --auto-write can capture the tree in
// a temporary index without the status walk or a lock on the user's index.
// Anything the guard would report still falls through so it prints before
// this command returns.
func queueBeforeStage(ctx context.Context, humanOut io.Writer, campRoot string, target *git.TargetResult) ([]string, bool, error) {
	priorRoots := git.DeclaredArtifactRoots(target.Path)
	if !commitAutoWrite || commitBackground || commitLarge {
		return priorRoots, false, nil
	}
	skip, err := commitSkipPaths(ctx, campRoot, target)
	if err != nil {
		return nil, false, err
	}
	// A skipped submodule pointer that is already staged would be reset from
	// HEAD by installing the captured tree. The foreground keeps it staged,
	// and refuses a projects/ pointer the way it always has.
	for _, path := range skip {
		if staged, stagedErr := git.HasStagedPathChange(ctx, target.Path, path); stagedErr != nil || staged {
			return priorRoots, false, stagedErr
		}
	}
	queued, err := cmdutil.TryQueueAutoWriteBeforeStage(
		ctx, humanOut, campRoot, target.Path, commitJSONOut, commitAmend, skip,
		defercommit.EnqueueOptions{
			WriterEnv:     workitemEnvForCommit(ctx, campRoot, commitWorkitem),
			MessagePrefix: commitTagPrefix(ctx, campRoot),
		})
	return priorRoots, queued, err
}

// reportStageOutcome acts on the guard's outcome, then prints what the stage
// kept out of the roots declared before this command and tracked growth
// inside them. It reports whether anything was kept out of those roots.
func reportStageOutcome(
	ctx context.Context, humanOut io.Writer, campRoot, repoPath string, priorRoots []string, outcome *git.StageOutcome,
) (*cmdutil.GuardHandling, bool, error) {
	handling, err := cmdutil.HandleStageOutcome(ctx, humanOut, campRoot, repoPath, outcome)
	if err != nil {
		return nil, false, err
	}
	reported := cmdutil.ReportRootsKept(humanOut, outcome)
	return handling, reported, cmdutil.ReportTrackedGrowthInRoots(ctx, humanOut, repoPath, priorRoots, commitLarge)
}

// nothingStaged reports whether the index holds nothing to commit once root
// content kept out of git is set aside.
func nothingStaged(ctx context.Context, repoPath string) (bool, error) {
	staged, err := git.HasStagedChanges(ctx, repoPath)
	return !staged, err
}

// commitSkipPaths lists what the fast path leaves out of the capture. Only
// a campaign-root commit without --include-refs leaves submodule pointers
// out; a nested repository or --include-refs records them, as the
// foreground stage does.
func commitSkipPaths(ctx context.Context, campRoot string, target *git.TargetResult) ([]string, error) {
	if target.IsNestedRepo() {
		return nil, nil
	}
	var paths []string
	if !commitIncludeRefs {
		subs, err := git.ListSubmodulePaths(ctx, target.Path)
		if err != nil {
			return nil, err
		}
		paths = subs
	}
	if wt := worktreesRelPath(ctx, campRoot); wt != "" {
		paths = append(paths, wt)
	}
	return paths, nil
}

func worktreesRelPath(ctx context.Context, campRoot string) string {
	worktreesPath := config.DefaultCampaignPaths().Worktrees
	if cfg, err := config.LoadCampaignConfig(ctx, campRoot); err == nil {
		if wt := cfg.Paths().Worktrees; wt != "" {
			worktreesPath = wt
		}
	}
	return strings.Trim(strings.TrimSpace(worktreesPath), "/")
}
