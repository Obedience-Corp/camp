package prune

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// MergedPRHeads maps a branch name to the head commits of its pull requests
// that were merged into base.
type MergedPRHeads func(ctx context.Context, projectPath, base string) (map[string][]string, error)

const (
	mergedPRLookupTimeout = 20 * time.Second
	mergedPRLookupLimit   = "1000"
)

// GitHubMergedPRHeads asks the gh CLI for the project's merged pull requests.
// It returns an error when gh is missing, not signed in, or the project is not
// on GitHub; callers treat that as "no answer" and keep the branch.
func GitHubMergedPRHeads(ctx context.Context, projectPath, base string) (map[string][]string, error) {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return nil, camperrors.Wrap(err, "gh is not installed")
	}
	ctx, cancel := context.WithTimeout(ctx, mergedPRLookupTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gh, "pr", "list", "--state", "merged", "--base", base,
		"--limit", mergedPRLookupLimit, "--json", "headRefName,headRefOid")
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, camperrors.Wrapf(err, "gh pr list: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, camperrors.Wrap(err, "gh pr list")
	}
	var prs []struct {
		HeadRefName string `json:"headRefName"`
		HeadRefOid  string `json:"headRefOid"`
	}
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, camperrors.Wrap(err, "parse gh pr list output")
	}
	heads := make(map[string][]string, len(prs))
	for _, pr := range prs {
		heads[pr.HeadRefName] = append(heads[pr.HeadRefName], pr.HeadRefOid)
	}
	return heads, nil
}

// partitionByMergedPR moves a branch out of unsafe when one of its merged pull
// requests had the branch's local tip as its head, or a descendant of it. The
// local branch then holds nothing the merge did not.
func partitionByMergedPR(ctx context.Context, path, baseRef string, unsafe []string, lookup MergedPRHeads, pr *ProjectResult) (merged, stillUnsafe []string) {
	if lookup == nil || len(unsafe) == 0 {
		return nil, unsafe
	}
	base := strings.TrimPrefix(baseRef, "origin/")
	heads, err := lookup(ctx, path, base)
	if err != nil {
		pr.Notes = append(pr.Notes, "merged pull requests were not checked: "+err.Error())
		return nil, unsafe
	}
	for _, b := range unsafe {
		tip, err := git.Output(ctx, path, "rev-parse", "--verify", "refs/heads/"+b)
		if err != nil || !tipInMergedPR(ctx, path, tip, heads[b]) {
			stillUnsafe = append(stillUnsafe, b)
			continue
		}
		merged = append(merged, b)
	}
	return merged, stillUnsafe
}

func tipInMergedPR(ctx context.Context, path, tip string, heads []string) bool {
	for _, head := range heads {
		if head == tip {
			return true
		}
		if ok, err := git.IsAncestor(ctx, path, tip, head); err == nil && ok {
			return true
		}
	}
	return false
}
