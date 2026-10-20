package git

import (
	"context"
	"strings"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/stageguard"
)

// planStageRoots plans a stage-everything commit's declared-root content.
// block refuses a commit that would leave an over-threshold file out of git,
// inside a declared root as anywhere else.
func planStageRoots(ctx context.Context, repoPath string, roots []string, admitLarge bool) (RootPlan, error) {
	if len(roots) == 0 {
		return RootPlan{}, nil
	}
	plan, limits, err := planDeclaredRoots(ctx, repoPath, roots, admitLarge)
	if err != nil {
		return RootPlan{}, err
	}
	if limits != nil && limits.LargeFiles == stageguard.ModeBlock && len(plan.Kept) > 0 {
		return RootPlan{}, &GuardBlockedError{
			Kind: stageguard.OverThreshold, Violations: plan.KeptViolations(), Limits: *limits, RepoPath: repoPath,
		}
	}
	return plan, nil
}

// excludeStageRoots adds the declared-root exclusions to a stage pathspec.
func excludeStageRoots(ctx context.Context, repoPath string, roots, staged []string) ([]string, error) {
	if len(roots) == 0 {
		return staged, nil
	}
	rootExcludes, err := rootExcludePathspecs(ctx, repoPath, roots)
	if err != nil {
		return nil, err
	}
	return applyGuardExclusions(staged, rootExcludes), nil
}

// finishStageRoots stages the declared roots' planned content after the main
// add, pairs over-threshold kept files with deleted tracked paths, and puts
// the kept-out tally on the outcome.
func finishStageRoots(ctx context.Context, repoPath string, roots []string, plan RootPlan, outcome *StageOutcome) (*StageOutcome, error) {
	if len(roots) == 0 {
		return outcome, nil
	}
	if err := stageInsideDeclaredRoots(ctx, repoPath, roots, plan); err != nil {
		return nil, err
	}
	if len(plan.Kept) > 0 {
		deleted, err := StagedDeletions(ctx, repoPath)
		if err != nil {
			return nil, err
		}
		if plan.Moved, err = PairMovesIntoRoots(ctx, repoPath, plan.Kept, deleted, nil); err != nil {
			return nil, err
		}
	}
	if tally := plan.Tally(roots); len(tally) > 0 || len(plan.Moved) > 0 {
		if outcome == nil {
			outcome = &StageOutcome{}
		}
		outcome.RootsKept = tally
		outcome.RootsMoved = plan.Moved
	}
	return outcome, nil
}

// planDeclaredRoots sizes the untracked content of declared roots under the
// repository's guard policy, and returns that policy. A policy that cannot be
// read keeps everything in the roots out, and the guard reports itself
// unavailable.
func planDeclaredRoots(ctx context.Context, repoPath string, roots []string, admitLarge bool) (RootPlan, *stageguard.GuardLimits, error) {
	limits, err := stageguard.ResolveLimits(ctx, repoPath)
	if err != nil {
		plan, planErr := PlanRootContent(ctx, repoPath, roots, nil, admitLarge)
		return plan, nil, planErr
	}
	plan, err := PlanRootContent(ctx, repoPath, roots, &limits, admitLarge)
	return plan, &limits, err
}

// stageInsideDeclaredRoots stages what a stage-everything add leaves behind
// when it excludes declared artifact roots so their media stays out.
//
// Edits and deletions of tracked files inside a root, such as a script beside
// the footage, still belong in the commit. So do the new files the root plan
// commits, which is also how a tracked file renamed inside a root or moved
// into one keeps its new path.
func stageInsideDeclaredRoots(ctx context.Context, repoPath string, roots []string, plan RootPlan) error {
	var trackedRoots []string
	for _, root := range roots {
		if artifacts.HasTrackedFiles(ctx, repoPath, root) {
			trackedRoots = append(trackedRoots, ":(literal)"+strings.TrimSuffix(root, "/"))
		}
	}
	if len(trackedRoots) > 0 {
		if err := StageTrackedChanges(ctx, repoPath, trackedRoots...); err != nil {
			return err
		}
	}
	if err := stageDirectoryLinks(ctx, repoPath, "", plan.Links, nil); err != nil {
		return err
	}
	paths := plan.CommitPaths()
	if len(paths) == 0 {
		return nil
	}
	cfg := DefaultRetryConfig()
	cfg.OperationName = "stage-root-files"
	for _, batch := range splitPathspecsForExec(paths, resetPathspecPayloadLimit(repoPath)) {
		pathspecs := make([]string, 0, len(batch)+1)
		pathspecs = append(pathspecs, "--")
		for _, path := range batch {
			pathspecs = append(pathspecs, ":(literal)"+path)
		}
		if err := WithLockRetry(ctx, repoPath, cfg, func() error {
			return executeStage(ctx, repoPath, pathspecs)
		}); err != nil {
			return err
		}
	}
	return nil
}
