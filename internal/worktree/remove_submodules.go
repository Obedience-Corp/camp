package worktree

import (
	"bufio"
	"context"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// gitlinkMode is the index mode git records for a submodule entry.
const gitlinkMode = "160000"

// RemoveSubmoduleAware removes a worktree, including one that contains
// submodules. Git refuses to remove such a worktree without --force no matter
// how clean it is, and --force also skips git's dirty-tree check. So the check
// git would have made is made here first: a worktree holding uncommitted work,
// or submodule commits that exist nowhere else, is refused; a clean one is
// removed with --force. forcedForSubmodules reports that second case so the
// caller can say so.
func (g *GitWorktree) RemoveSubmoduleAware(ctx context.Context, path string, force bool) (forcedForSubmodules bool, err error) {
	if force {
		return false, g.Remove(ctx, path, true)
	}

	hasSubmodules, err := g.worktreeHasSubmodules(ctx, path)
	if err != nil || !hasSubmodules {
		// Without a positive answer, git's own checks stay in charge.
		return false, g.Remove(ctx, path, false)
	}

	if err := g.verifyNothingUnsaved(ctx, path); err != nil {
		return false, err
	}
	return true, g.Remove(ctx, path, true)
}

// worktreeHasSubmodules mirrors the test git applies before refusing: any
// gitlink in the worktree's index, initialized or not.
func (g *GitWorktree) worktreeHasSubmodules(ctx context.Context, path string) (bool, error) {
	out, err := g.runIn(ctx, path, "ls-files", "--stage")
	if err != nil {
		return false, err
	}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), gitlinkMode+" ") {
			return true, nil
		}
	}
	return false, nil
}

// verifyNothingUnsaved fails when removing the worktree would destroy work.
// A linked worktree keeps its submodule repositories inside its own git dir,
// so commits made in a submodule and never pushed vanish with the worktree
// even when the superproject looks clean.
func (g *GitWorktree) verifyNothingUnsaved(ctx context.Context, path string) error {
	project := filepath.Base(g.projectPath)

	status, err := g.runIn(ctx, path, "status", "--porcelain", "--ignore-submodules=none")
	if err != nil {
		return GitOperationFailed(project, "status", err)
	}
	if strings.TrimSpace(status) != "" {
		return camperrors.Wrapf(camperrors.ErrInvalidInput,
			"worktree %s has uncommitted changes; commit them, or pass --force to discard them:\n%s",
			path, strings.TrimRight(status, "\n"))
	}

	unpushed, err := g.runIn(ctx, path, "submodule", "foreach", "--recursive", "--quiet",
		`c=$(git rev-list --max-count=1 HEAD --branches --not --remotes); [ -z "$c" ] || echo "$displaypath $c"`)
	if err != nil {
		return GitOperationFailed(project, "submodule foreach", err)
	}
	if strings.TrimSpace(unpushed) != "" {
		return camperrors.Wrapf(camperrors.ErrInvalidInput,
			"worktree %s has submodule commits that are on no remote and would be lost; push them, or pass --force to discard them:\n%s",
			path, strings.TrimRight(unpushed, "\n"))
	}
	return nil
}

func (g *GitWorktree) runIn(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", parseGitError(err, output)
	}
	return string(output), nil
}
