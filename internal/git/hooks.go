package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	switch classifyCommitHooks(ctx, repoPath) {
	case commitHooksNone, commitHooksDirection:
		return false
	default:
		return true
	}
}

// OnlyDirectionShim reports whether the only executable commit hook is the
// fest-direction commit-msg shim.
//
// commit-tree does not run hooks. The commit-tree worker uses this to decide
// whether to append that shim's trailers before creating the commit. An
// unreadable hooks directory is not this case.
func OnlyDirectionShim(ctx context.Context, repoPath string) bool {
	return classifyCommitHooks(ctx, repoPath) == commitHooksDirection
}

type commitHookClass int

const (
	commitHooksUnknown commitHookClass = iota
	commitHooksNone
	commitHooksDirection
	commitHooksBlocking
)

func classifyCommitHooks(ctx context.Context, repoPath string) commitHookClass {
	dir, ok := hooksDirectory(ctx, repoPath)
	if !ok {
		return commitHooksUnknown
	}
	shim := false
	blocking := false
	for _, name := range commitHookNames {
		path := filepath.Join(dir, name)
		if !executableHook(path) {
			continue
		}
		if name == "commit-msg" && isDirectionShim(path) {
			shim = true
			continue
		}
		blocking = true
	}
	if shim && executableHook(filepath.Join(dir, directionChainedHook)) {
		blocking = true
	}
	switch {
	case blocking:
		return commitHooksBlocking
	case shim:
		return commitHooksDirection
	default:
		return commitHooksNone
	}
}

func hooksDirectory(ctx context.Context, repoPath string) (string, bool) {
	out, err := Output(ctx, repoPath, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return "", false
	}
	dir := strings.TrimSpace(out)
	if dir == "" {
		return "", false
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoPath, dir)
	}
	return dir, true
}

func executableHook(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	// The executable bit is what git itself requires, so a disabled hook left
	// in place as a .sample or with its bit cleared does not force the whole
	// repository back to synchronous commits.
	return info.Mode().Perm()&0o111 != 0
}

// isDirectionShim reports whether path is the fest-direction commit-msg shim.
//
// The marker alone is not enough: a user's hook could mention it. The shim
// also execs `fest-direction hook commit-msg` or, in older installs,
// `direction hook commit-msg`. A file that cannot be read is not the shim.
func isDirectionShim(path string) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	marker := false
	execLine := false
	for line := range strings.SplitSeq(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == directionHookMarker || strings.HasPrefix(line, directionHookMarker+" ") {
			marker = true
		}
		if directionExecLine(line) {
			execLine = true
		}
	}
	return marker && execLine
}

func directionExecLine(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[0] != "exec" {
		return false
	}
	switch filepath.Base(fields[1]) {
	case "fest-direction", "direction":
	default:
		return false
	}
	return fields[2] == "hook" && fields[3] == "commit-msg"
}
