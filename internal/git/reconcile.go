package git

import (
	"context"
	"fmt"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// RefDivergence reports how many commits are unique to each side of a ref
// comparison. Ahead is unique to localRef; Behind is unique to remoteRef.
type RefDivergence struct {
	Ahead  int
	Behind int
}

// CompareRefs compares two refs without changing either one.
func CompareRefs(ctx context.Context, repoPath, localRef, remoteRef string) (RefDivergence, error) {
	output, err := RunGitCmd(ctx, repoPath, "rev-list", "--left-right", "--count", localRef+"..."+remoteRef)
	if err != nil {
		return RefDivergence{}, err
	}

	var divergence RefDivergence
	if _, err := fmt.Sscanf(output, "%d %d", &divergence.Ahead, &divergence.Behind); err != nil {
		return RefDivergence{}, camperrors.Wrapf(err,
			"parse divergence between %s and %s: %s", localRef, remoteRef, strings.TrimSpace(output))
	}
	return divergence, nil
}

// CreateBranchRef creates a local branch at startPoint without checking it out.
func CreateBranchRef(ctx context.Context, repoPath, branch, startPoint string) error {
	_, err := RunGitCmd(ctx, repoPath, "branch", branch, startPoint)
	return err
}

// FastForwardTo advances the checked-out branch to ref without creating a
// merge commit or reconciling divergent history.
func FastForwardTo(ctx context.Context, repoPath, ref string) error {
	_, err := RunGitCmd(ctx, repoPath, "merge", "--ff-only", ref)
	return err
}

// ResetHardTo makes the checked-out branch and worktree match ref. Callers
// must establish their own clean-worktree and recovery-ref safety guarantees.
func ResetHardTo(ctx context.Context, repoPath, ref string) error {
	_, err := RunGitCmd(ctx, repoPath, "reset", "--hard", ref)
	return err
}
