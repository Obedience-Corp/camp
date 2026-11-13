package prune

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/skills"
	"github.com/Obedience-Corp/camp/internal/worktree"
)

const (
	// SavedEditsMaxBytes caps what prune will save from one worktree. Larger
	// uncommitted files keep the worktree in place.
	SavedEditsMaxBytes = 50 << 20

	// RecentEditWindow keeps a dirty worktree whose files changed this
	// recently. Uncommitted edits on a merged branch are usually leftovers,
	// but they can be the start of follow-up work, so only a worktree nobody
	// has touched for this long is treated as abandoned.
	RecentEditWindow = 14 * 24 * time.Hour
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

	// Set for actionSaveAndRemove. ignoredFiles are the files git ignores one
	// by one (local config, secrets, editor leftovers); they are saved with
	// the edits. ignoredDirs are folders an ignore rule names as a whole
	// (dependencies, build output); they are deleted with the worktree and
	// not saved, so the prompt and the result name them.
	ignoredFiles []string
	ignoredDirs  []string
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
	// A linked worktree keeps its submodule repositories in its own git dir,
	// so commits made there and never pushed go with it, clean or not. No
	// flag discards them: DiscardDirty is about uncommitted changes.
	unpushed, err := worktree.NewGitWorktree(projectPath).UnpushedSubmoduleCommits(ctx, entry.Path)
	if err != nil {
		return worktreePlan{action: actionError, reason: fmt.Sprintf("submodule check for worktree: %s", err)}
	}
	if unpushed != "" {
		return worktreePlan{action: actionKeep, reason: "worktree with submodule commits on no remote: " + entry.Path}
	}
	if opts.DiscardDirty {
		return worktreePlan{action: actionRemove}
	}
	// --ignored=matching reports a folder an ignore rule names as one entry
	// and never lists its contents, so a dependency tree costs one record.
	status, err := git.Output(ctx, entry.Path, "status", "--porcelain=v2", "-z",
		"--untracked-files=all", "--ignored=matching", "--ignore-submodules=none")
	if err != nil {
		return worktreePlan{action: actionError, reason: fmt.Sprintf("dirty check for worktree: %s", err)}
	}
	changes, ignored := splitIgnored(parseStatusV2(status))
	keep := func(why string) worktreePlan {
		return worktreePlan{action: actionKeep, reason: why + ": " + entry.Path, skip: SkipReasonDirtyWorktree}
	}
	if len(changes) == 0 {
		return worktreePlan{action: actionRemove}
	}
	if !opts.SaveDirty {
		return keep("dirty worktree")
	}
	if op := operationInProgress(ctx, entry.Path); op != "" {
		return keep("dirty worktree with a " + op + " in progress")
	}
	plan := worktreePlan{action: actionSaveAndRemove}
	var bytes int64
	var newest time.Time
	seen := func(t time.Time) {
		if t.After(newest) {
			newest = t
		}
	}
	for _, c := range changes {
		if c.submoduleDirty {
			return keep("dirty worktree with uncommitted changes inside submodule " + c.path)
		}
		if strings.HasSuffix(c.path, "/") {
			// Git reports an untracked folder as one entry only when it is a
			// repository of its own. A save would record a pointer to it and
			// none of its files.
			return keep("dirty worktree holding another git repository at " + c.path)
		}
		full := filepath.Join(entry.Path, c.path)
		info, err := os.Lstat(full)
		if err != nil {
			// Deleted in the worktree. The nearest folder that still exists
			// lost an entry at that moment, so its time records the deletion
			// even when whole folders went.
			if t, ok := nearestExistingModTime(entry.Path, full); ok {
				seen(t)
			}
			continue
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		}
		seen(info.ModTime())
	}
	for _, c := range ignored {
		full := filepath.Join(entry.Path, c.path)
		if strings.HasSuffix(c.path, "/") {
			if _, err := os.Lstat(filepath.Join(full, ".git")); err == nil {
				return keep("dirty worktree holding another git repository at " + c.path)
			}
			// A folder's own time says little about its contents, so it is
			// not evidence of recent work. Folders camp projects into every
			// worktree are its own to recreate and are not worth naming.
			if !skills.IsWorktreeProjectionDir(c.path) {
				plan.ignoredDirs = append(plan.ignoredDirs, c.path)
			}
			continue
		}
		info, err := os.Lstat(full)
		if err != nil {
			continue
		}
		if info.Mode().IsRegular() {
			bytes += info.Size()
		}
		seen(info.ModTime())
		plan.ignoredFiles = append(plan.ignoredFiles, c.path)
	}
	if bytes > SavedEditsMaxBytes {
		return keep(fmt.Sprintf("dirty worktree with %d MiB of uncommitted and ignored files (over the %d MiB save limit)",
			bytes>>20, SavedEditsMaxBytes>>20))
	}
	if newest.IsZero() {
		return keep("dirty worktree whose last edit could not be dated")
	}
	if age := now.Sub(newest); age < RecentEditWindow {
		return keep("dirty worktree edited " + formatAge(age) + " ago")
	}
	if _, err := git.Output(ctx, projectPath, "rev-parse", "--verify", "--quiet", savedRefName(branch)); err == nil {
		return keep("dirty worktree, and earlier saved edits for this branch still exist")
	}
	return plan
}

// nearestExistingModTime returns the modification time of the closest ancestor
// of a deleted path that still exists, looking no higher than root.
func nearestExistingModTime(root, deleted string) (time.Time, bool) {
	for dir := filepath.Dir(deleted); ; dir = filepath.Dir(dir) {
		if info, err := os.Lstat(dir); err == nil {
			return info.ModTime(), true
		}
		if dir == root || len(dir) <= len(root) {
			return time.Time{}, false
		}
	}
}

// ignoredDirsNote names the ignored folders a removal deletes without saving,
// or is empty when there are none.
func ignoredDirsNote(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	const show = 4
	names := dirs
	more := ""
	if len(dirs) > show {
		names = dirs[:show]
		more = fmt.Sprintf(" and %d more", len(dirs)-show)
	}
	return "ignored folders not saved: " + strings.Join(names, ", ") + more
}

type statusChange struct {
	path           string
	submoduleDirty bool
	ignored        bool
}

func splitIgnored(all []statusChange) (changes, ignored []statusChange) {
	for _, c := range all {
		if c.ignored {
			ignored = append(ignored, c)
		} else {
			changes = append(changes, c)
		}
	}
	return changes, ignored
}

// parseStatusV2 reads `git status --porcelain=v2 -z`. It reports each changed,
// untracked or ignored path, and whether a submodule has modified or untracked
// content of its own, which a commit in the parent cannot capture.
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
		case '!':
			changes = append(changes, statusChange{path: strings.TrimPrefix(rec, "! "), ignored: true})
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

func formatAge(age time.Duration) string {
	switch {
	case age >= 48*time.Hour:
		return fmt.Sprintf("%d days", int(age.Hours()/24))
	case age >= 2*time.Hour:
		return fmt.Sprintf("%d hours", int(age.Hours()))
	default:
		return age.Round(time.Minute).String()
	}
}
