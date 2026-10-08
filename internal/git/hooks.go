package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// commitHookNames are the hooks that run during an ordinary `git commit`.
//
// Only these three. A repository with a `pre-push` hook can still defer
// commits, because nothing about deferring changes when a push happens.
var commitHookNames = []string{"pre-commit", "prepare-commit-msg", "commit-msg"}

// directionHookMarker is the line fest-direction writes into its commit-msg
// shim. The stock line continues with a description after the marker; older
// shims use the marker alone.
const directionHookMarker = "# direction-hook v1"

// directionChainedHook is the foreign commit-msg the stock shim runs first
// when it is executable. It is user code. commit-tree would skip it, so its
// presence keeps deferral in the foreground.
const directionChainedHook = "commit-msg.before-direction"

// HasCommitHooks reports whether deferral must stay in the foreground because
// of a commit hook.
//
// A hook is the user's own code, and it expects to run at commit time, in the
// foreground, against the tree being committed. Deferring past it would either
// skip it silently or run it minutes later against a different working tree,
// and both are worse than not deferring.
//
// The fest-direction commit-msg shim is the exception. Its only effect, when
// nothing is chained in front of it, is trailers of the tree being committed,
// and the commit-tree worker appends those itself. Any other executable
// pre-commit, prepare-commit-msg, or commit-msg still counts.
//
// The hooks directory comes from git rather than being assumed, so a repo that
// sets core.hooksPath, or a worktree whose hooks live in the parent's common
// directory, resolves correctly without camp knowing the rules. When that
// directory cannot be read, this reports that hooks exist: the expensive
// mistake is skipping a hook the user wrote, not committing in the foreground.
func HasCommitHooks(ctx context.Context, repoPath string) bool {
	class, err := classifyCommitHooks(ctx, repoPath)
	if err != nil {
		return true
	}
	switch class {
	case commitHooksNone, commitHooksDirection:
		return false
	default:
		return true
	}
}

// OnlyDirectionShim reports whether deferred commits must reproduce direction
// trailers. No hooks returns false, nil. Other hooks or inspection failures
// return an error: commit-tree cannot safely reproduce their behavior.
func OnlyDirectionShim(ctx context.Context, repoPath string) (bool, error) {
	class, err := classifyCommitHooks(ctx, repoPath)
	if err != nil {
		return false, err
	}
	switch class {
	case commitHooksNone:
		return false, nil
	case commitHooksDirection:
		return true, nil
	default:
		return false, camperrors.New("commit hooks require a foreground commit")
	}
}

type commitHookClass int

const (
	commitHooksUnknown commitHookClass = iota
	commitHooksNone
	commitHooksDirection
	commitHooksBlocking
)

func classifyCommitHooks(ctx context.Context, repoPath string) (commitHookClass, error) {
	if err := ctx.Err(); err != nil {
		return commitHooksUnknown, err
	}
	dir, err := hooksDirectory(ctx, repoPath)
	if err != nil {
		return commitHooksUnknown, err
	}
	shim := false
	blocking := false
	for _, name := range commitHookNames {
		path := filepath.Join(dir, name)
		executable, err := executableHook(path)
		if err != nil {
			return commitHooksUnknown, err
		}
		if !executable {
			continue
		}
		if name == "commit-msg" && isDirectionShim(path) {
			shim = true
			continue
		}
		blocking = true
	}
	if shim {
		executable, err := executableHook(filepath.Join(dir, directionChainedHook))
		if err != nil {
			return commitHooksUnknown, err
		}
		blocking = blocking || executable
	}
	switch {
	case blocking:
		return commitHooksBlocking, nil
	case shim:
		return commitHooksDirection, nil
	default:
		return commitHooksNone, nil
	}
}

func hooksDirectory(ctx context.Context, repoPath string) (string, error) {
	out, err := Output(ctx, repoPath, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return "", camperrors.Wrap(err, "resolve commit hooks directory")
	}
	dir := strings.TrimSpace(out)
	if dir == "" {
		return "", camperrors.New("git returned an empty commit hooks directory")
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoPath, dir)
	}
	return dir, nil
}

func executableHook(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, camperrors.Wrapf(err, "inspect commit hook %s", path)
	}
	if info.IsDir() {
		return false, nil
	}
	// The executable bit is what git itself requires, so a disabled hook left
	// in place as a .sample or with its bit cleared does not force the whole
	// repository back to synchronous commits.
	return info.Mode().Perm()&0o111 != 0, nil
}

// isDirectionShim accepts only the supported shim bodies. Recognizing a marker
// and exec anywhere in a script would silently bypass additional user logic.
func isDirectionShim(path string) bool {
	body, err := os.ReadFile(path)
	return err == nil && directionShimBody(string(body))
}

func directionShimBody(body string) bool {
	lines := strings.SplitN(body, "\n", 3)
	if len(lines) != 3 || lines[0] != "#!/bin/sh" {
		return false
	}
	if lines[1] != directionHookMarker && !strings.HasPrefix(lines[1], directionHookMarker+" ") {
		return false
	}

	// Current shims optionally chain a previous hook; classification separately
	// checks that the chained file is not executable. Older shims exec directly.
	const chain = "here=\"$(dirname \"$0\")\"\n" +
		"if [ -x \"$here/commit-msg.before-direction\" ]; then \"$here/commit-msg.before-direction\" \"$@\" || exit $?; fi\n"
	script := strings.TrimSuffix(strings.TrimPrefix(lines[2], chain), "\n")
	switch script {
	case `exec fest-direction hook commit-msg "$1"`,
		`exec fest-direction hook commit-msg "$@"`,
		`exec direction hook commit-msg "$1"`,
		`exec direction hook commit-msg "$@"`:
		return true
	default:
		return false
	}
}
