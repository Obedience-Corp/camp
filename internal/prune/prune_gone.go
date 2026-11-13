package prune

import (
	"context"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/git"
)

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
