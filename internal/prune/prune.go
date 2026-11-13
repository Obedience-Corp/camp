package prune

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

// Status represents the outcome of a single branch prune operation.
type Status string

// SkipReason identifies why a prune result was skipped.
type SkipReason string

const (
	StatusDeleted             Status = "deleted"
	StatusWouldDelete         Status = "would delete"
	StatusSkipped             Status = "skipped"
	StatusError               Status = "error"
	StatusWouldPrune          Status = "would prune"
	StatusWorktreeRemoved     Status = "wt removed"
	StatusWorktreeWouldRemove Status = "wt would remove"
	StatusEditsSaved          Status = "edits saved"
	StatusEditsWouldSave      Status = "would save edits"
	StatusSavedExpired        Status = "saved edits expired"

	SkipReasonActiveWorktree SkipReason = "active_worktree"
	SkipReasonDirtyWorktree  SkipReason = "dirty_worktree"
)

// Options holds configuration for a prune operation.
type Options struct {
	DryRun       bool
	Force        bool
	Remote       bool
	RemoteDelete bool
	BaseRef      string

	// SkipWorktreeBranches preserves merged branches that still have active
	// worktrees instead of removing the worktree and deleting the branch.
	SkipWorktreeBranches bool

	// DiscardDirty allows removal of worktrees that have uncommitted changes
	// (for the branch-worktree removal path). When false (default), dirty
	// branch worktrees are skipped with SkipReasonDirtyWorktree.
	DiscardDirty bool

	// SaveDirty removes a merged branch's worktree even when it holds
	// uncommitted edits, after saving those edits under SavedRefPrefix. When
	// false, such a worktree is kept, as it also is when the edits cannot be
	// saved safely (see planWorktree). DiscardDirty takes precedence.
	SaveDirty bool

	// MergedPRs, when set, is asked about branches whose upstream is gone but
	// whose commits are not patch-equivalent to BaseRef, the usual result of
	// a squash merge after the default branch moved on.
	MergedPRs MergedPRHeads

	// RefreshRemote causes Execute to run 'git fetch --prune <remote>'
	// before checking upstream:track state. Required for gone-upstream
	// detection to reflect the current remote. Set true for interactive
	// entrypoints (camp fresh, camp project prune) and false for library
	// callers that manage their own fetching.
	RefreshRemote bool

	// Remote name used when RefreshRemote is true. Empty means "origin".
	RemoteName string

	// PreserveDetachedWorktrees lists detached worktree paths that this prune
	// pass must retain even when their commits are merged into BaseRef. This is
	// used when a caller detached a worktree as part of the surrounding
	// operation and must not immediately undo that work by deleting it.
	PreserveDetachedWorktrees []string

	// PreviewPrimaryAtBase applies only together with DryRun. Set it when the
	// real pass runs after the caller has moved the primary worktree to
	// BaseRef, as camp fresh does: it checks out the default branch, or
	// detaches at origin/<default>, and syncs before it prunes. A dry-run has
	// done none of that, so without this the preview protects the branch the
	// primary worktree still sits on and under-reports what the real pass
	// deletes. With it, that branch is judged like any other and the primary
	// worktree is never reported as a worktree to remove for it.
	PreviewPrimaryAtBase bool
}

// previewsPrimaryAtBase reports whether this pass is a preview taken from the
// primary worktree's post-sync position rather than its current one.
func (o Options) previewsPrimaryAtBase() bool {
	return o.DryRun && o.PreviewPrimaryAtBase
}

// Result holds the outcome for a single branch.
type Result struct {
	Branch     string
	Status     Status
	Detail     string
	SkipReason SkipReason
}

// ProjectResult holds all results for a single project.
type ProjectResult struct {
	Name    string
	Path    string
	Results []Result
	Pruned  int // remote refs pruned
	Error   string
	// Notes are facts about the pass that belong to no single branch.
	Notes []string
}

// Execute runs the prune logic for a single project.
func Execute(ctx context.Context, name, path string, opts Options) ProjectResult {
	pr := ProjectResult{Name: name, Path: path}

	baseRef := strings.TrimSpace(opts.BaseRef)
	if baseRef == "" {
		baseRef = git.DefaultBranch(ctx, path)
		if baseRef == "" {
			pr.Error = "could not determine default branch"
			return pr
		}
	}

	// Refresh remote tracking first when the caller asked for it, so
	// upstream:track state reflects the current remote. Without this,
	// gone-upstream detection would be stale for anyone who hadn't
	// manually run 'git fetch --prune'.
	if opts.RefreshRemote {
		remote := opts.RemoteName
		if remote == "" {
			remote = "origin"
		}
		if err := git.FetchRemotePrune(ctx, path, remote); err != nil {
			pr.Results = append(pr.Results, Result{
				Branch: "(fetch --prune)",
				Status: StatusError,
				Detail: fmt.Sprintf("refresh %s: %s", remote, err),
			})
			// Continue — ancestry-merged detection still works.
		}
	}

	listMerged, listGone := git.MergedBranchesFromRef, git.GoneBranches
	if opts.previewsPrimaryAtBase() {
		listMerged, listGone = git.MergedBranchesFromRefIncludingCurrent, git.GoneBranchesIncludingCurrent
	}

	merged, err := listMerged(ctx, path, baseRef)
	if err != nil {
		pr.Error = err.Error()
		return pr
	}

	// Gone-upstream branches catch squash-merged PRs that git's ancestry
	// check misses. But gone-upstream alone is ambiguous: someone who
	// deleted the remote branch without merging produces the same signal.
	// Filter through a merge-equivalence check (cumulative patch-id of the
	// branch matches a commit's self-diff on baseRef) before force-deleting.
	gone, err := listGone(ctx, path)
	if err != nil {
		pr.Results = append(pr.Results, Result{
			Branch: "(gone upstream)",
			Status: StatusError,
			Detail: fmt.Sprintf("list gone branches: %s", err),
		})
		gone = nil
	}
	// A branch git already sees as merged needs no second opinion, and must
	// not also be reported as kept.
	gone = withoutBranches(gone, merged)
	equivalentGone, unsafeGone := partitionGoneByMergeEquivalence(ctx, path, baseRef, gone, &pr)
	mergedByPR, unsafeGone := partitionByMergedPR(ctx, path, baseRef, unsafeGone, opts.MergedPRs, &pr)
	equivalentGone = append(equivalentGone, mergedByPR...)
	if opts.SaveDirty && !opts.DryRun {
		expired, err := expireSaved(ctx, path, time.Now())
		for _, b := range expired {
			pr.Results = append(pr.Results, Result{Branch: b, Status: StatusSavedExpired,
				Detail: fmt.Sprintf("saved more than %d days ago", int(SavedRetention.Hours()/24))})
		}
		if err != nil {
			pr.Results = append(pr.Results, Result{Branch: "(saved edits)", Status: StatusError, Detail: err.Error()})
		}
	}

	// Record gone-but-not-merge-equivalent branches as skipped so the user
	// sees why they were not auto-deleted.
	for _, b := range unsafeGone {
		detail := "gone upstream but local commits are not merge-equivalent to " + baseRef
		if opts.DryRun {
			detail = "would keep: " + detail
		}
		pr.Results = append(pr.Results, Result{
			Branch: b,
			Status: StatusSkipped,
			Detail: detail,
		})
	}

	candidates, forced := unionBranches(merged, equivalentGone)

	deleteDetachedWorktrees(ctx, path, baseRef, opts, &pr)
	if len(candidates) == 0 && !opts.Remote {
		return pr
	}

	deleteLocalBranches(ctx, path, candidates, forced, opts, &pr)

	// Remote branch deletion uses only the ancestry-merged list — a
	// gone-upstream branch has already been deleted upstream, so there is
	// nothing to delete on origin.
	deleteRemoteBranches(ctx, path, merged, opts, &pr)

	pruneTrackingRefs(ctx, path, opts, &pr)

	return pr
}

// partitionGoneByMergeEquivalence splits gone-upstream branches into two
// sets: those whose cumulative diff matches a commit on baseRef (safe to
// force-delete — a squash-merge signature) and those that do not (unsafe,
// likely a remote-deleted unmerged branch with local-only work).
//
// Per-branch patch-ids on baseRef are computed once across the superset of
// all branches' merge-bases, then each branch's cumulative patch-id is
// checked against that set. One log+patch-id pair per call.
func partitionGoneByMergeEquivalence(ctx context.Context, path, baseRef string, gone []string, pr *ProjectResult) (equivalent, unsafe []string) {
	if len(gone) == 0 {
		return nil, nil
	}

	// Find the oldest merge-base across all gone branches so we scan the
	// smallest sufficient range on baseRef.
	var oldestBase string
	branchBases := make(map[string]string, len(gone))
	for _, b := range gone {
		mb, err := git.MergeBase(ctx, path, baseRef, b)
		if err != nil || mb == "" {
			// Can't check — treat as unsafe.
			unsafe = append(unsafe, b)
			continue
		}
		branchBases[b] = mb
		if oldestBase == "" {
			oldestBase = mb
			continue
		}
		// Pick whichever of oldestBase/mb is ancestor of the other and
		// keep the older one. If neither is ancestor (divergent history
		// within gone branches), fall back to oldestBase — the base-side
		// set will still cover the usual case.
		if isAnc, _ := git.IsAncestor(ctx, path, mb, oldestBase); isAnc {
			oldestBase = mb
		}
	}

	if oldestBase == "" {
		return nil, unsafe
	}

	basePatchIDs, err := git.BasePatchIDSet(ctx, path, oldestBase, baseRef)
	if err != nil {
		// Can't build the equivalence set — fall back to treating all
		// gone branches as unsafe rather than risk losing local work.
		pr.Results = append(pr.Results, Result{
			Branch: "(merge-equivalence)",
			Status: StatusError,
			Detail: fmt.Sprintf("compute base patch-ids for %s: %s", baseRef, err),
		})
		for b := range branchBases {
			unsafe = append(unsafe, b)
		}
		return nil, unsafe
	}

	for _, b := range gone {
		mb, ok := branchBases[b]
		if !ok {
			continue // already in unsafe
		}
		id, err := git.CumulativePatchID(ctx, path, mb, b)
		if err != nil || id == "" {
			unsafe = append(unsafe, b)
			continue
		}
		if _, hit := basePatchIDs[id]; hit {
			equivalent = append(equivalent, b)
		} else {
			unsafe = append(unsafe, b)
		}
	}
	return equivalent, unsafe
}

// unionBranches merges a branches list from ancestry (git --merged) with one
// from gone-upstream tracking, dedup'd, returning the combined list plus a
// set of branch names that must be force-deleted (i.e. came only from the
// gone path, where git's -d safe-delete would refuse because the branch is
// squash-merged rather than ancestry-merged).
func unionBranches(merged, gone []string) ([]string, map[string]struct{}) {
	mergedSet := make(map[string]struct{}, len(merged))
	for _, b := range merged {
		mergedSet[b] = struct{}{}
	}
	forced := make(map[string]struct{})

	out := make([]string, 0, len(merged)+len(gone))
	out = append(out, merged...)
	for _, b := range gone {
		if _, already := mergedSet[b]; already {
			continue
		}
		out = append(out, b)
		forced[b] = struct{}{}
	}
	return out, forced
}

func withoutBranches(branches, exclude []string) []string {
	if len(branches) == 0 || len(exclude) == 0 {
		return branches
	}
	skip := make(map[string]struct{}, len(exclude))
	for _, b := range exclude {
		skip[b] = struct{}{}
	}
	kept := make([]string, 0, len(branches))
	for _, b := range branches {
		if _, drop := skip[b]; !drop {
			kept = append(kept, b)
		}
	}
	return kept
}

func deleteDetachedWorktrees(ctx context.Context, path, baseRef string, opts Options, pr *ProjectResult) {
	wt := worktree.NewGitWorktree(path)
	entries, err := wt.List(ctx)
	if err != nil {
		pr.Results = append(pr.Results, Result{
			Branch: "(detached worktrees)",
			Status: StatusError,
			Detail: fmt.Sprintf("worktree list: %s", err),
		})
		return
	}

	skipPaths := map[string]struct{}{
		filepath.Clean(path): {},
	}
	if gitDir, err := git.Output(ctx, path, "rev-parse", "--absolute-git-dir"); err == nil {
		skipPaths[filepath.Clean(gitDir)] = struct{}{}
	}
	for _, preserved := range opts.PreserveDetachedWorktrees {
		if preserved = strings.TrimSpace(preserved); preserved != "" {
			skipPaths[filepath.Clean(preserved)] = struct{}{}
		}
	}

	removedAny := false
	for _, entry := range entries {
		if !entry.IsDetached || entry.Commit == "" || entry.IsBare || entry.IsLocked {
			continue
		}

		if _, skip := skipPaths[filepath.Clean(entry.Path)]; skip {
			continue
		}

		if _, err := os.Lstat(entry.Path); os.IsNotExist(err) {
			// The folder is gone; only git's registration is left.
			result := Result{Branch: formatDetachedWorktreeLabel(entry.Commit)}
			if opts.DryRun {
				result.Status = StatusWorktreeWouldRemove
				result.Detail = "folder already gone, registration would be dropped: " + entry.Path
			} else {
				result.Status = StatusWorktreeRemoved
				result.Detail = "folder was already gone, registration dropped: " + entry.Path
				removedAny = true
			}
			pr.Results = append(pr.Results, result)
			continue
		}

		merged, err := git.IsAncestor(ctx, path, entry.Commit, baseRef)
		if err != nil {
			pr.Results = append(pr.Results, Result{
				Branch: formatDetachedWorktreeLabel(entry.Commit),
				Status: StatusError,
				Detail: fmt.Sprintf("merge-base check for %s: %s", entry.Path, err),
			})
			continue
		}
		if !merged {
			continue
		}

		clean, err := detachedWorktreeClean(ctx, entry.Path)
		if err != nil {
			pr.Results = append(pr.Results, Result{
				Branch: formatDetachedWorktreeLabel(entry.Commit),
				Status: StatusError,
				Detail: fmt.Sprintf("dirty check for %s: %s", entry.Path, err),
			})
			continue
		}
		if !clean {
			detail := fmt.Sprintf("dirty detached worktree: %s", entry.Path)
			if opts.DryRun {
				detail = fmt.Sprintf("would keep dirty detached worktree: %s", entry.Path)
			}
			pr.Results = append(pr.Results, Result{
				Branch:     formatDetachedWorktreeLabel(entry.Commit),
				Status:     StatusSkipped,
				Detail:     detail,
				SkipReason: SkipReasonDirtyWorktree,
			})
			continue
		}

		result := Result{
			Branch: formatDetachedWorktreeLabel(entry.Commit),
			Detail: entry.Path,
		}

		if opts.DryRun {
			result.Status = StatusWorktreeWouldRemove
			pr.Results = append(pr.Results, result)
			continue
		}

		if err := wt.Remove(ctx, entry.Path, true); err != nil {
			result.Status = StatusError
			result.Detail = fmt.Sprintf("%s: %s", entry.Path, err)
			pr.Results = append(pr.Results, result)
			continue
		}

		result.Status = StatusWorktreeRemoved
		pr.Results = append(pr.Results, result)
		removedAny = true
	}

	if !opts.DryRun && removedAny {
		appendWorktreePruneError(ctx, wt, pr)
	}
}

func formatDetachedWorktreeLabel(commit string) string {
	const shortSHA = 7
	if len(commit) > shortSHA {
		commit = commit[:shortSHA]
	}
	return "detached@" + commit
}

func detachedWorktreeClean(ctx context.Context, path string) (bool, error) {
	hasChanges, err := git.HasChanges(ctx, path)
	if err != nil {
		return false, err
	}
	return !hasChanges, nil
}

func appendWorktreePruneError(ctx context.Context, wt *worktree.GitWorktree, pr *ProjectResult) {
	if _, err := wt.Prune(ctx, false); err != nil {
		pr.Results = append(pr.Results, Result{
			Branch: "(worktree admin)",
			Status: StatusError,
			Detail: fmt.Sprintf("worktree prune: %s", err),
		})
	}
}

// detectWorktreesForBranches lists worktrees and returns a map of branch name → worktree entry
// for branches that appear in the merged set.
func detectWorktreesForBranches(ctx context.Context, path string, merged []string) map[string]worktree.GitWorktreeEntry {
	wt := worktree.NewGitWorktree(path)
	entries, err := wt.List(ctx)
	if err != nil {
		return nil
	}

	mergedSet := make(map[string]struct{}, len(merged))
	for _, b := range merged {
		mergedSet[b] = struct{}{}
	}

	result := make(map[string]worktree.GitWorktreeEntry)
	for _, e := range entries {
		if _, ok := mergedSet[e.Branch]; ok {
			result[e.Branch] = e
		}
	}
	return result
}

// deleteLocalBranches handles confirmation and deletion of locally merged
// branches. If a branch has an active worktree, the worktree is removed
// first. Entries listed in forced (branches whose upstream is gone but
// which git's ancestry check does not see as merged) are deleted with -D
// instead of -d.
func deleteLocalBranches(ctx context.Context, path string, merged []string, forced map[string]struct{}, opts Options, pr *ProjectResult) {
	if len(merged) == 0 {
		return
	}

	wtMap := detectWorktreesForBranches(ctx, path, merged)
	if opts.previewsPrimaryAtBase() {
		// The worktree holding the current branch is the primary worktree
		// itself. The real pass runs after it has left that branch, so it is
		// not a worktree to remove.
		delete(wtMap, git.CurrentBranch(ctx, path))
	}
	branchesToDelete := make([]string, 0, len(merged))
	plans := make(map[string]worktreePlan)
	now := time.Now()

	// Decide every worktree before asking anything, so the confirmation lists
	// exactly what the pass will do.
	for _, branch := range merged {
		entry, hasWT := wtMap[branch]
		if hasWT && opts.SkipWorktreeBranches {
			detail := fmt.Sprintf("active worktree: %s", entry.Path)
			if opts.DryRun {
				detail = fmt.Sprintf("would keep active worktree: %s", entry.Path)
			}
			pr.Results = append(pr.Results, Result{
				Branch:     branch,
				Status:     StatusSkipped,
				Detail:     detail,
				SkipReason: SkipReasonActiveWorktree,
			})
			continue
		}
		if hasWT {
			plan := planWorktree(ctx, path, entry, branch, opts, now)
			switch plan.action {
			case actionKeep:
				detail := plan.reason
				if opts.DryRun {
					detail = "would keep " + detail
				}
				pr.Results = append(pr.Results, Result{Branch: branch, Status: StatusSkipped, Detail: detail, SkipReason: plan.skip})
				continue
			case actionError:
				pr.Results = append(pr.Results, Result{Branch: branch, Status: StatusError, Detail: plan.reason})
				continue
			}
			plans[branch] = plan
		}
		branchesToDelete = append(branchesToDelete, branch)
	}

	if len(branchesToDelete) == 0 {
		return
	}

	if !opts.DryRun && !opts.Force {
		fmt.Printf("\n%s Will delete %d merged or gone-upstream branch(es) in %s:\n",
			ui.WarningIcon(), len(branchesToDelete), ui.Value(pr.Name))
		for _, b := range branchesToDelete {
			suffix := ""
			if _, isForced := forced[b]; isForced {
				suffix = " (gone upstream — force delete)"
			}
			if plan, hasWT := plans[b]; hasWT {
				suffix += " " + promptWorktreeNote(plan.action)
			}
			fmt.Printf("  %s %s%s\n", ui.Dim("-"), b, suffix)
		}
		fmt.Print("\nProceed? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if !strings.HasPrefix(strings.ToLower(answer), "y") {
			for _, b := range branchesToDelete {
				pr.Results = append(pr.Results, Result{
					Branch: b,
					Status: StatusSkipped,
					Detail: "cancelled by user",
				})
			}
			return
		}
	}

	wt := worktree.NewGitWorktree(path)
	skipBranchDelete := make(map[string]struct{})
	for _, branch := range branchesToDelete {
		plan, hasWT := plans[branch]
		if !hasWT {
			continue
		}
		results, ok := removePlannedWorktree(ctx, wt, path, branch, wtMap[branch], plan, opts)
		pr.Results = append(pr.Results, results...)
		if !ok {
			skipBranchDelete[branch] = struct{}{}
		}
	}

	// Clean stale worktree refs after removals.
	if !opts.DryRun && len(plans) > 0 {
		appendWorktreePruneError(ctx, wt, pr)
	}

	for _, branch := range branchesToDelete {
		if _, skip := skipBranchDelete[branch]; skip {
			continue
		}

		detail := ""
		if _, isForced := forced[branch]; isForced {
			detail = "gone upstream"
		}

		if opts.DryRun {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusWouldDelete,
				Detail: detail,
			})
			continue
		}

		var deleteErr error
		if _, isForced := forced[branch]; isForced {
			deleteErr = git.DeleteBranchForce(ctx, path, branch)
		} else {
			deleteErr = git.DeleteBranch(ctx, path, branch)
		}

		if deleteErr != nil {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusError,
				Detail: deleteErr.Error(),
			})
		} else {
			pr.Results = append(pr.Results, Result{
				Branch: branch,
				Status: StatusDeleted,
				Detail: detail,
			})
		}
	}
}

// deleteRemoteBranches handles confirmation and deletion of merged branches on origin.
func deleteRemoteBranches(ctx context.Context, path string, merged []string, opts Options, pr *ProjectResult) {
	if !opts.RemoteDelete || len(merged) == 0 {
		return
	}

	if opts.DryRun {
		for _, branch := range merged {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusWouldDelete,
				Detail: "remote",
			})
		}
		return
	}

	// Always confirm remote deletion independently — --force only covers local
	fmt.Printf("\n%s Will DELETE %d branch(es) from origin (irreversible):\n",
		ui.WarningIcon(), len(merged))
	for _, b := range merged {
		fmt.Printf("  %s origin/%s\n", ui.Dim("-"), b)
	}
	fmt.Print("\nDelete from remote? [y/N] ")
	var answer string
	fmt.Scanln(&answer)
	if !strings.HasPrefix(strings.ToLower(answer), "y") {
		for _, branch := range merged {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusSkipped,
				Detail: "remote deletion cancelled",
			})
		}
		return
	}

	for _, branch := range merged {
		if err := git.DeleteRemoteBranch(ctx, path, branch); err != nil {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusError,
				Detail: err.Error(),
			})
		} else {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusDeleted,
				Detail: "remote",
			})
		}
	}
}

// pruneTrackingRefs handles pruning of stale remote tracking refs.
func pruneTrackingRefs(ctx context.Context, path string, opts Options, pr *ProjectResult) {
	if !opts.Remote {
		return
	}

	if opts.DryRun {
		pr.Results = append(pr.Results, Result{
			Branch: "(remote tracking refs)",
			Status: StatusWouldPrune,
		})
		return
	}

	count, err := git.PruneRemote(ctx, path)
	if err != nil {
		pr.Results = append(pr.Results, Result{
			Branch: "(remote tracking refs)",
			Status: StatusError,
			Detail: err.Error(),
		})
	} else {
		pr.Pruned = count
	}
}

func promptWorktreeNote(action worktreeAction) string {
	switch action {
	case actionSaveAndRemove:
		return "(has worktree — uncommitted edits will be saved, then it is removed)"
	case actionDropRegistration:
		return "(worktree folder is already gone — its registration will be dropped)"
	default:
		return "(has worktree — will be removed)"
	}
}

// removePlannedWorktree carries out one worktree plan. ok is false when the
// worktree is still there, in which case its branch must be kept too.
//
// A dirty worktree is removed only after its edits are saved and read back. No
// failure here falls back to deleting anything.
func removePlannedWorktree(ctx context.Context, wt *worktree.GitWorktree, projectPath, branch string, entry worktree.GitWorktreeEntry, plan worktreePlan, opts Options) (results []Result, ok bool) {
	restore := "restore with: camp project prune --restore " + branch
	if opts.DryRun {
		switch plan.action {
		case actionSaveAndRemove:
			results = append(results, Result{Branch: branch, Status: StatusEditsWouldSave, Detail: entry.Path})
		case actionDropRegistration:
			return []Result{{Branch: branch, Status: StatusWorktreeWouldRemove, Detail: "folder already gone, registration would be dropped: " + entry.Path}}, true
		}
		return append(results, Result{Branch: branch, Status: StatusWorktreeWouldRemove, Detail: entry.Path}), true
	}

	if plan.action == actionDropRegistration {
		if _, err := wt.Prune(ctx, false); err != nil {
			return []Result{{Branch: branch, Status: StatusError, Detail: fmt.Sprintf("worktree prune: %s", err)}}, false
		}
		return []Result{{Branch: branch, Status: StatusWorktreeRemoved, Detail: "folder was already gone, registration dropped: " + entry.Path}}, true
	}

	savedRef := ""
	if plan.action == actionSaveAndRemove {
		ref, err := saveWorktreeEdits(ctx, projectPath, entry.Path, branch)
		if err != nil {
			return []Result{{Branch: branch, Status: StatusSkipped, SkipReason: SkipReasonDirtyWorktree,
				Detail: fmt.Sprintf("dirty worktree kept, its edits could not be saved (%s): %s", err, entry.Path)}}, false
		}
		savedRef = ref
	}

	if err := wt.Remove(ctx, entry.Path, true); err != nil {
		detail := fmt.Sprintf("worktree remove: %s", err)
		if savedRef != "" {
			// The worktree may be partly removed, so the saved copy stays.
			detail += "; its edits are saved, " + restore + " once the folder is cleared"
		}
		return []Result{{Branch: branch, Status: StatusError, Detail: detail}}, false
	}
	if _, err := os.Lstat(entry.Path); err == nil {
		detail := "git removed the worktree but files remain in " + entry.Path
		if savedRef != "" {
			detail += "; its edits are saved, " + restore + " once the folder is cleared"
		}
		return []Result{{Branch: branch, Status: StatusError, Detail: detail}}, false
	}

	if savedRef != "" {
		results = append(results, Result{Branch: branch, Status: StatusEditsSaved, Detail: restore})
	}
	return append(results, Result{Branch: branch, Status: StatusWorktreeRemoved, Detail: entry.Path}), true
}
