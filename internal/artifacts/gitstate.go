package artifacts

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
)

// The two questions every caller asks about an artifact root's relationship to
// git. They live here rather than in the CLI because three consumers need the
// same answers: `camp artifacts add` deciding whether it may write a gitignore
// rule, the doctor checks reporting root health, and the staging path deciding
// what to exclude. Three copies of a git invocation would drift.

// IsGitignored reports whether git ignores rel, which is the desired state for
// a clean artifact root.
//
// It asks git rather than reimplementing the rules, so negation patterns,
// nested .gitignore files, and excludesfile all resolve the way they do
// everywhere else. A failed invocation reports false: the caller's safe
// direction is to assume content is still visible to git.
func IsGitignored(ctx context.Context, campRoot, rel string) bool {
	check := exec.CommandContext(ctx, "git", "-C", campRoot, "check-ignore", "-q", "--",
		filepath.FromSlash(NormalizeRootPath(rel)))
	return check.Run() == nil
}

// HasTrackedFiles reports whether git tracks anything under rel, which makes
// the root "mixed": the same bytes would be both git content and artifact
// content. A mixed root must never be blanket-gitignored, because that would
// hide tracked files from git without removing them.
func HasTrackedFiles(ctx context.Context, campRoot, rel string) bool {
	ls := exec.CommandContext(ctx, "git", "-C", campRoot, "ls-files", "--",
		filepath.FromSlash(NormalizeRootPath(rel)))
	out, err := ls.Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}
