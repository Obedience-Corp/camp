package git

import (
	"context"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// BranchesEquivalentToRef reports which of branches already have their
// changes on baseRef. A branch qualifies if it is an ancestor of baseRef
// (git merge-base --is-ancestor) or if its cumulative patch-id matches a
// commit on baseRef — the squash-merge / cherry-pick signature.
//
// This is stronger than MergedBranchesFromRef, which is ancestry-only and
// therefore misses GitHub squash-merges. git cherry is not used: it compares
// per-commit patch-ids and misses a multi-commit branch squashed into one
// commit. Uncheckable branches (missing merge-base, empty diff, patch-id
// error) are omitted; they are not treated as equivalent.
func BranchesEquivalentToRef(ctx context.Context, repoPath, baseRef string, branches []string) (map[string]struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(baseRef) == "" {
		return nil, camperrors.Newf("base ref is required")
	}

	out := make(map[string]struct{})
	if len(branches) == 0 {
		return out, nil
	}

	remaining, err := ancestryEquivalent(ctx, repoPath, baseRef, branches, out)
	if err != nil {
		return nil, err
	}
	if len(remaining) == 0 {
		return out, nil
	}

	squashed, err := patchEquivalentBranches(ctx, repoPath, baseRef, remaining)
	if err != nil {
		return nil, err
	}
	for b := range squashed {
		out[b] = struct{}{}
	}
	return out, nil
}

// ancestryEquivalent records branches that are ancestors of baseRef and
// returns the rest for squash/patch-id checking.
func ancestryEquivalent(ctx context.Context, repoPath, baseRef string, branches []string, out map[string]struct{}) ([]string, error) {
	remaining := make([]string, 0, len(branches))
	for _, b := range branches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if b == "" || b == baseRef {
			continue
		}
		isAnc, err := IsAncestor(ctx, repoPath, b, baseRef)
		if err != nil {
			remaining = append(remaining, b)
			continue
		}
		if isAnc {
			out[b] = struct{}{}
			continue
		}
		remaining = append(remaining, b)
	}
	return remaining, nil
}

// patchEquivalentBranches returns branches whose cumulative diff from their
// merge-base with baseRef matches a commit on baseRef.
func patchEquivalentBranches(ctx context.Context, repoPath, baseRef string, branches []string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if len(branches) == 0 {
		return out, nil
	}

	branchBases, oldest, err := mergeBasesForBranches(ctx, repoPath, baseRef, branches)
	if err != nil {
		return nil, err
	}
	if oldest == "" {
		return out, nil
	}

	basePatchIDs, err := BasePatchIDSet(ctx, repoPath, oldest, baseRef)
	if err != nil {
		return nil, camperrors.Wrapf(err, "compute patch-ids on %s", baseRef)
	}

	for _, b := range branches {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mb, ok := branchBases[b]
		if !ok {
			continue
		}
		id, idErr := CumulativePatchID(ctx, repoPath, mb, b)
		if idErr != nil || id == "" {
			continue
		}
		if _, hit := basePatchIDs[id]; hit {
			out[b] = struct{}{}
		}
	}
	return out, nil
}

// mergeBasesForBranches maps each branch to merge-base(baseRef, branch) and
// returns the oldest of those bases so one BasePatchIDSet covers them all.
func mergeBasesForBranches(ctx context.Context, repoPath, baseRef string, branches []string) (map[string]string, string, error) {
	branchBases := make(map[string]string, len(branches))
	var oldest string
	for _, b := range branches {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		mb, err := MergeBase(ctx, repoPath, baseRef, b)
		if err != nil || mb == "" {
			continue
		}
		branchBases[b] = mb
		oldest = olderCommit(ctx, repoPath, oldest, mb)
	}
	return branchBases, oldest, nil
}

// olderCommit returns the ancestor of a and b when they are linearly related;
// otherwise it keeps a (already chosen) so the scan range still covers the
// common case.
func olderCommit(ctx context.Context, repoPath, a, b string) string {
	if a == "" {
		return b
	}
	if isAnc, _ := IsAncestor(ctx, repoPath, b, a); isAnc {
		return b
	}
	return a
}
