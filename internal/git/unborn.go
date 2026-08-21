package git

import (
	"bytes"
	"context"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// EmptyTreeSHA is the tree of a repository with no files.
//
// An unborn HEAD compares equal to this: there is no parent commit, so
// "nothing to commit" is "the staged tree is the empty tree".
func EmptyTreeSHA(ctx context.Context, repoPath string) (string, error) {
	cmd := gitCmd(ctx, repoPath, "hash-object", "-t", "tree", "--stdin")
	cmd.Stdin = bytes.NewReader(nil)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", camperrors.Wrapf(err, "hash the empty tree: %s", detail)
		}
		return "", camperrors.Wrap(err, "hash the empty tree")
	}
	sha := strings.TrimSpace(string(out))
	if sha == "" {
		return "", camperrors.New("git hash-object produced no empty tree")
	}
	return sha, nil
}

// zeroOID is the all-zeros object name for this repository's hash algorithm.
//
// `git update-ref` uses it as the "current value" when creating a ref: the
// move succeeds only if the ref still does not exist. That is the unborn-HEAD
// compare-and-swap — the same guarantee UpdateHeadFrom makes when oldSHA is a
// real parent. Length comes from the empty tree so SHA-1 and SHA-256 repos
// both get the right width without parsing object-format names.
func zeroOID(ctx context.Context, repoPath string) (string, error) {
	sha, err := EmptyTreeSHA(ctx, repoPath)
	if err != nil {
		return "", err
	}
	return strings.Repeat("0", len(sha)), nil
}

// expectedHEAD is the current-value argument `git update-ref` needs to move
// HEAD to a deferred commit. Empty oldSHA is unborn HEAD: the all-zeros OID,
// so the move fails if someone else already created the first commit.
func expectedHEAD(ctx context.Context, repoPath, oldSHA string) (string, error) {
	if strings.TrimSpace(oldSHA) != "" {
		return oldSHA, nil
	}
	return zeroOID(ctx, repoPath)
}
