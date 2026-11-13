package prune

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

const (
	// SavedEditsMaxBytes caps what prune will save from one worktree. Larger
	// uncommitted files keep the worktree in place.
	SavedEditsMaxBytes = 50 << 20

	// RecentEditWindow keeps a dirty worktree whose files changed this
	// recently: it is probably still in use.
	RecentEditWindow = 24 * time.Hour
)

type worktreeAction int

const (
	// actionRemove removes a clean worktree (or a dirty one under DiscardDirty).
	actionRemove worktreeAction = iota
	// actionSaveAndRemove saves the uncommitted edits, then removes it.
	actionSaveAndRemove
	// actionDropRegistration clears a registration whose folder is gone.
	actionDropRegistration
	// actionKeep leaves the worktree and its branch alone.
	actionKeep
	// actionError reports a failed check; the worktree and branch are kept.
	actionError
)

// worktreePlan is the single decision for a merged branch's worktree. The dry
// run, the confirmation prompt and the real pass all read it, so they agree.
type worktreePlan struct {
	action worktreeAction
	reason string // why it is kept, or the failed check
	skip   SkipReason
}

func planWorktree(ctx context.Context, projectPath string, entry worktree.GitWorktreeEntry, branch string, opts Options, now time.Time) worktreePlan {
	if _, err := os.Lstat(entry.Path); os.IsNotExist(err) {
		if entry.IsLocked {
			return worktreePlan{action: actionKeep, reason: "locked worktree whose folder is gone: " + entry.Path}
		}
		return worktreePlan{action: actionDropRegistration}
	}
	if entry.IsLocked {
		return worktreePlan{action: actionKeep, reason: "locked worktree: " + entry.Path}
	}
	if opts.DiscardDirty {
		return worktreePlan{action: actionRemove}
	}
	status, err := git.Output(ctx, entry.Path, "status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return worktreePlan{action: actionError, reason: fmt.Sprintf("dirty check for worktree: %s", err)}
	}
	changes := parseStatusV2(status)
	if len(changes) == 0 {
		return worktreePlan{action: actionRemove}
	}
	keep := func(why string) worktreePlan {
		return worktreePlan{action: actionKeep, reason: why + ": " + entry.Path, skip: SkipReasonDirtyWorktree}
	}
	if !opts.SaveDirty {
		return keep("dirty worktree")
	}
	if op := operationInProgress(ctx, entry.Path); op != "" {
		return keep("dirty worktree with a " + op + " in progress")
	}
	var bytes int64
	var newest time.Time
	for _, c := range changes {
		if c.submoduleDirty {
			return keep("dirty worktree with uncommitted changes inside submodule " + c.path)
		}
		info, err := os.Lstat(filepath.Join(entry.Path, c.path))
		if err != nil {
			continue // deleted in the worktree
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if bytes > SavedEditsMaxBytes {
		return keep(fmt.Sprintf("dirty worktree with %d MiB of uncommitted files (over the %d MiB save limit)",
			bytes>>20, SavedEditsMaxBytes>>20))
	}
	if age := now.Sub(newest); !newest.IsZero() && age < RecentEditWindow {
		return keep(fmt.Sprintf("dirty worktree edited %s ago", age.Round(time.Minute)))
	}
	if unpushed, err := worktree.NewGitWorktree(projectPath).UnpushedSubmoduleCommits(ctx, entry.Path); err != nil {
		return worktreePlan{action: actionError, reason: fmt.Sprintf("submodule check for worktree: %s", err)}
	} else if unpushed != "" {
		return keep("dirty worktree with submodule commits on no remote")
	}
	if _, err := git.Output(ctx, projectPath, "rev-parse", "--verify", "--quiet", savedRefName(branch)); err == nil {
		return keep("dirty worktree, and earlier saved edits for this branch still exist")
	}
	return worktreePlan{action: actionSaveAndRemove}
}

type statusChange struct {
	path           string
	submoduleDirty bool
}

// parseStatusV2 reads `git status --porcelain=v2 -z`. It reports each changed
// or untracked path, and whether a submodule has modified or untracked content
// of its own, which a commit in the parent cannot capture.
func parseStatusV2(out string) []statusChange {
	var changes []statusChange
	records := strings.Split(out, "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if rec == "" {
			continue
		}
		switch rec[0] {
		case '1':
			f := strings.SplitN(rec, " ", 9)
			if len(f) == 9 {
				changes = append(changes, statusChange{path: f[8], submoduleDirty: submoduleContentDirty(f[2])})
			}
		case '2':
			f := strings.SplitN(rec, " ", 10)
			if len(f) == 10 {
				changes = append(changes, statusChange{path: f[9], submoduleDirty: submoduleContentDirty(f[2])})
			}
			i++ // the original path of a rename follows as its own record
		case 'u':
			f := strings.SplitN(rec, " ", 11)
			if len(f) == 11 {
				changes = append(changes, statusChange{path: f[10], submoduleDirty: submoduleContentDirty(f[2])})
			}
		case '?':
			changes = append(changes, statusChange{path: strings.TrimPrefix(rec, "? ")})
		}
	}
	return changes
}

// submoduleContentDirty reads the porcelain v2 submodule field "S<c><m><u>":
// m is modified tracked content, u is untracked content.
func submoduleContentDirty(field string) bool {
	return len(field) == 4 && field[0] == 'S' && (field[2] == 'M' || field[3] == 'U')
}

func operationInProgress(ctx context.Context, wtPath string) string {
	for marker, name := range map[string]string{
		"MERGE_HEAD":       "merge",
		"rebase-merge":     "rebase",
		"rebase-apply":     "rebase",
		"CHERRY_PICK_HEAD": "cherry-pick",
		"REVERT_HEAD":      "revert",
		"BISECT_LOG":       "bisect",
	} {
		p, err := git.Output(ctx, wtPath, "rev-parse", "--path-format=absolute", "--git-path", marker)
		if err != nil {
			continue
		}
		if _, err := os.Lstat(p); err == nil {
			return name
		}
	}
	return ""
}
