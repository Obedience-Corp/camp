package workitem

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	campgit "github.com/Obedience-Corp/camp/internal/git"
)

// assertCleanIndex refuses to proceed when repoRoot has pre-existing staged
// changes. Protects against the kill-mid-commit dirty-index trap where the
// user's reflex `git reset --hard` would destroy the staged work. The hint
// explicitly recommends `git reset HEAD` (which preserves the worktree) over
// `--hard`, and points the user at `--staged` for the deliberate variant.
func assertCleanIndex(ctx context.Context, repoRoot string) error {
	staged, err := listStagedFiles(ctx, repoRoot)
	if err != nil {
		return camperrors.Wrap(err, "check staged index")
	}
	if len(staged) == 0 {
		return nil
	}
	preview := staged
	if len(preview) > 5 {
		preview = preview[:5]
	}
	msg := fmt.Sprintf("repo %s has %d pre-existing staged file(s); run `git reset HEAD` to unstage them (preserves the worktree; NOT --hard which discards uncommitted work), or re-run with --staged to commit the existing index. Sample: %s",
		repoRoot, len(staged), strings.Join(preview, ", "))
	return camperrors.NewValidation("staged_index", msg, nil)
}

// listChangedFilesUnder returns repo-relative paths for changes inside prefix
// (or the entire repo when prefix is empty). Wraps `git status --porcelain`
// with `--untracked-files=all` so untracked directories are expanded to
// individual files (otherwise --exclude cannot target leaf paths).
func listChangedFilesUnder(ctx context.Context, repoRoot, prefix string) ([]string, error) {
	args := []string{"--untracked-files=all"}
	if prefix != "" {
		args = append(args, "--", prefix)
	}
	out, err := campgit.StatusPorcelain(ctx, repoRoot, args...)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range campgit.ParseStatusPorcelainZ(out) {
		files = append(files, entry.Path)
	}
	return files, nil
}

func listStagedFiles(ctx context.Context, repoRoot string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "diff", "--cached", "--name-status", "-z").CombinedOutput()
	if err != nil {
		return nil, camperrors.NewGit("diff --cached --name-status", "", "", strings.TrimSpace(string(out)), err)
	}
	return campgit.ParseDiffNameStatusZ(out)
}

func pathIsDirty(ctx context.Context, repoRoot, relPath string) (bool, error) {
	out, err := campgit.StatusPorcelain(ctx, repoRoot, "--", relPath)
	if err != nil {
		return false, err
	}
	return len(campgit.ParseStatusPorcelainZ(out)) != 0, nil
}

func listDirtySubmodulePointers(ctx context.Context, repoRoot string) ([]string, error) {
	out, err := campgit.StatusPorcelain(ctx, repoRoot, "--", "projects")
	if err != nil {
		return nil, err
	}
	var pointers []string
	for _, entry := range campgit.ParseStatusPorcelainZ(out) {
		if strings.HasPrefix(entry.Path, "projects/") {
			pointers = append(pointers, entry.Path)
		}
	}
	return pointers, nil
}

func listSubmodulePointerSkips(ctx context.Context, root string, allowed bool) []SkippedEntry {
	if allowed {
		return nil
	}
	pointers, err := listDirtySubmodulePointers(ctx, root)
	if err != nil {
		return nil
	}
	out := make([]SkippedEntry, 0, len(pointers))
	for _, p := range pointers {
		out = append(out, SkippedEntry{Path: p, Reason: skipReasonPointerOffByDefault})
	}
	return out
}
