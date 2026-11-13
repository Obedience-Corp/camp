package fresh

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

func resolveFreshSyncState(ctx context.Context, path, defaultBranch string) (freshSyncState, error) {
	state := freshSyncState{
		defaultBranch: defaultBranch,
		baseRef:       defaultBranch,
		displayRef:    defaultBranch,
	}

	// Scan for another worktree holding the default branch. When this path is
	// detached or on a feature branch, main often sits stuck on a finished
	// worktree left over from camp project worktree add --start-point main.
	entry, err := findBranchInOtherWorktree(ctx, path, defaultBranch)
	if err != nil {
		return state, err
	}
	if entry == nil {
		return state, nil
	}

	// Prefer reclaim + normal checkout; until reclaim succeeds, plan for a
	// detached origin/<default> sync so fresh does not hard-fail.
	state.baseRef = "origin/" + defaultBranch
	state.displayRef = state.baseRef + " (detached)"
	state.detached = true
	state.worktreePath = entry.Path

	return state, nil
}

// maybeReclaimDefaultBranch frees the default branch from another clean
// worktree when possible. Dry-run still inspects dirtiness (status is
// non-mutating) so the printed plan matches a real run. On success, mutates
// state so the primary path checks out the real default branch; on dirty
// skip, leaves the detached-origin fallback in place.
func maybeReclaimDefaultBranch(ctx context.Context, state *freshSyncState, dryRun bool) error {
	if state == nil || state.worktreePath == "" {
		return nil
	}

	var (
		reclaimed bool
		err       error
	)
	if dryRun {
		reclaimed, err = canReclaimDefaultBranchWorktree(ctx, state.worktreePath)
	} else {
		reclaimed, err = reclaimDefaultBranchWorktree(ctx, state.worktreePath)
	}
	if err != nil {
		return err
	}
	if reclaimed {
		applyReclaimDecision(state)
	}
	printReclaimStep(*state, reclaimed, dryRun)
	return nil
}

// applyReclaimDecision flips a detached-fallback plan to a normal default-branch
// checkout after the occupying worktree has been (or would be) freed.
func applyReclaimDecision(state *freshSyncState) {
	if state == nil {
		return
	}
	state.reclaimed = true
	state.detached = false
	state.baseRef = state.defaultBranch
	state.displayRef = state.defaultBranch
}

// reclaimStepDetail returns the step label and dim detail for a free/reclaim
// line. Pure so dry-run and real paths cannot drift and unit tests can lock copy.
func reclaimStepDetail(branch, worktreePath string, reclaimed, dryRun bool) (label, detail string) {
	base := filepath.Base(worktreePath)
	if dryRun {
		label = "Would free " + branch
		if reclaimed {
			detail = fmt.Sprintf("(detach clean worktree %s)", base)
		} else {
			detail = fmt.Sprintf("skipped · %s has uncommitted changes; would sync detached", base)
		}
		return label, detail
	}
	label = "Free " + branch
	if reclaimed {
		detail = fmt.Sprintf("(detached %s so %s is free here)", base, branch)
	} else {
		detail = fmt.Sprintf("skipped · %s has uncommitted changes; syncing detached", base)
	}
	return label, detail
}

func printReclaimStep(state freshSyncState, reclaimed, dryRun bool) {
	label, detail := reclaimStepDetail(state.defaultBranch, state.worktreePath, reclaimed, dryRun)
	if !dryRun && reclaimed {
		freshRow(label, "done", ui.StatusSuccess)
		freshDetail(detail)
		return
	}
	// Dry-run, or a dirty worktree that keeps the detached-sync fallback.
	freshRow(label, detail, ui.StatusMuted)
}

// findBranchInOtherWorktree returns the first worktree (other than path) that
// has branch checked out. path is the project working tree being freshened.
func findBranchInOtherWorktree(ctx context.Context, path, branch string) (*worktree.GitWorktreeEntry, error) {
	entries, err := worktree.NewGitWorktree(path).List(ctx)
	if err != nil {
		return nil, err
	}

	self := worktreeToplevel(ctx, path)
	for _, entry := range entries {
		if entry.Branch != branch {
			continue
		}
		// Skip the worktree that is path itself (path form can differ for
		// submodules: projects/camp vs .git/modules/projects/camp).
		if sameWorktreePath(self, worktreeToplevel(ctx, entry.Path)) {
			continue
		}
		entryCopy := entry
		return &entryCopy, nil
	}

	return nil, nil
}

// canReclaimDefaultBranchWorktree reports whether occupyingPath is clean
// enough to detach without losing work (used by dry-run planning).
func canReclaimDefaultBranchWorktree(ctx context.Context, occupyingPath string) (bool, error) {
	dirty, err := git.HasNonSubmoduleChanges(ctx, occupyingPath)
	if err != nil {
		return false, camperrors.Wrapf(err, "check worktree %s for uncommitted changes", occupyingPath)
	}
	return !dirty, nil
}

// reclaimDefaultBranchWorktree detaches a clean worktree so its branch ref
// can be checked out elsewhere. Returns (false, nil) when the worktree has
// uncommitted changes and must not be touched; (true, nil) after a successful
// detach.
func reclaimDefaultBranchWorktree(ctx context.Context, occupyingPath string) (bool, error) {
	ok, err := canReclaimDefaultBranchWorktree(ctx, occupyingPath)
	if err != nil || !ok {
		return ok, err
	}
	// Detach at HEAD: keeps the same commit checked out, frees the branch name.
	if err := git.CheckoutDetached(ctx, occupyingPath, "HEAD"); err != nil {
		return false, camperrors.Wrapf(err, "detach worktree %s to free its branch", occupyingPath)
	}
	return true, nil
}

func worktreeToplevel(ctx context.Context, path string) string {
	out, err := git.Output(ctx, path, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(out) == "" {
		abs, absErr := filepath.Abs(path)
		if absErr != nil {
			return filepath.Clean(path)
		}
		return abs
	}
	return filepath.Clean(strings.TrimSpace(out))
}

func sameWorktreePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	// Resolve symlinks when possible (submodule / worktree path forms).
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA == nil && errB == nil {
		return ra == rb
	}
	return false
}

func emptyBranchLabel(branch string) string {
	if branch == "" {
		return "detached HEAD"
	}
	return branch
}

func isBranchInUseByWorktree(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already used by worktree") ||
		strings.Contains(msg, "is already checked out at")
}

func freshSyncWorktreeNote(state freshSyncState) string {
	if state.worktreePath == "" {
		return ""
	}
	return fmt.Sprintf("(%s still in %s)", state.defaultBranch, filepath.Base(state.worktreePath))
}
