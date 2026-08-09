package stageguard

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// detectNestedRepos reports which untracked directories would become orphaned
// gitlinks if staged.
//
// A directory qualifies when it is a git repository and .gitmodules does not
// declare it. Both halves matter. Without the repository test the guard would
// fire on ordinary directories git happened not to expand; without the
// .gitmodules test it would fire on a legitimate submodule the user is adding,
// which is the one case where staging a gitlink is exactly right.
//
// The declared set is read once for the whole batch rather than per directory,
// because the common case is zero nested repositories and the common cost
// should be one git call that returns nothing.
func detectNestedRepos(ctx context.Context, repoPath string, dirs []string, allow []string) ([]GuardViolation, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(dirs) == 0 {
		return nil, nil
	}

	declared, err := declaredSubmodulePaths(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	violations := make([]GuardViolation, 0, len(dirs))
	for _, dir := range dirs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if declared[dir] || MatchesAllow(dir, allow) {
			continue
		}
		if !isGitRepo(filepath.Join(repoPath, filepath.FromSlash(dir))) {
			continue
		}
		violations = append(violations, GuardViolation{
			Kind: NestedRepo,
			Path: dir,
			Head: nestedHead(ctx, filepath.Join(repoPath, filepath.FromSlash(dir))),
		})
	}
	if len(violations) == 0 {
		return nil, nil
	}
	return violations, nil
}

// isGitRepo reports whether dir holds a .git entry. It accepts both a directory
// (an ordinary clone) and a file (a worktree or submodule checkout, where .git
// is a gitdir pointer), because git records a gitlink for either and both are
// how the nested checkouts that motivated this guard actually arrive.
func isGitRepo(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// nestedHead returns the nested repository's HEAD in short form, or "" when it
// has none yet or cannot be read. It is reported for orientation only, so a
// failure to read it must never fail the guard.
func nestedHead(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--short", "HEAD")
	cmd.Env = gitEnv(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// declaredSubmodulePaths returns the submodule paths .gitmodules declares, as a
// set. A repository with no .gitmodules declares nothing, which is not an error:
// it is the ordinary state of most repositories and the one where this guard
// matters most.
//
// It shells out rather than calling internal/git.ListSubmodulePaths for the
// same reason Enumerate runs its own git status: internal/git imports this
// package, so importing it back would restore the cycle the leaf position
// exists to break.
func declaredSubmodulePaths(ctx context.Context, repoPath string) (map[string]bool, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	gitmodules := filepath.Join(repoPath, ".gitmodules")
	if _, err := os.Lstat(gitmodules); err != nil {
		return nil, nil
	}

	// -z terminates values with NUL, so a path containing whitespace or a
	// newline survives intact. Parsing the file by hand would have to reproduce
	// git's config syntax, including line continuations and quoting.
	cmd := exec.CommandContext(ctx, "git", "config", "-f", gitmodules, "-z", "--get-regexp", `^submodule\..*\.path$`)
	cmd.Env = gitEnv(os.Environ())

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// Exit code 1 means no key matched: a .gitmodules with no path entries.
		// That is a declaration of nothing, not a failure to read.
		var exitErr *exec.ExitError
		if camperrors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, camperrors.Wrapf(err, "read submodule paths from %s: %s",
			gitmodules, strings.TrimSpace(stderr.String()))
	}

	declared := make(map[string]bool)
	for _, record := range bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0}) {
		// Each record is "<key>\n<value>"; the value is the declared path.
		_, value, found := strings.Cut(string(record), "\n")
		if !found {
			continue
		}
		if path := strings.TrimSuffix(filepath.ToSlash(strings.TrimSpace(value)), "/"); path != "" {
			declared[path] = true
		}
	}
	return declared, nil
}
