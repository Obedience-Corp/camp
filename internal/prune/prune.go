package prune

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/git"
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
	// Note names what a worktree removal deletes without saving. It is also
	// part of Detail; callers that print their own summary read it here.
	Note string
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
