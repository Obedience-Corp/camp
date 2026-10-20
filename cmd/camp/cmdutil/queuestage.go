package cmdutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/defercommit"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/stageguard"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// TryQueueAutoWriteBeforeStage queues an --auto-write commit without walking
// declared artifact roots or staging the user's index.
//
// It returns true when the caller should stop. False means the ordinary
// foreground stage still has to run, because something new might need to be
// said out loud: a first artifact root, a nested repository, a bulk dump, or
// a tracked file that grew past the threshold.
func TryQueueAutoWriteBeforeStage(
	ctx context.Context,
	out io.Writer,
	campRoot, repoPath string,
	jsonOut, amend bool,
	skip []string,
	opts defercommit.EnqueueOptions,
) (bool, error) {
	allowed, _ := defercommit.Allowed(ctx, defercommit.Request{
		CampaignRoot: campRoot,
		RepoPath:     repoPath,
		JSON:         jsonOut,
		Amend:        amend,
	})
	if !allowed {
		return false, nil
	}

	tracked, err := git.ChangedNames(ctx, repoPath)
	if err != nil {
		return false, err
	}
	untracked, err := git.UntrackedCollapsed(ctx, repoPath)
	if err != nil {
		return false, err
	}
	limits, err := stageguard.ResolveLimits(ctx, repoPath)
	if err != nil {
		return false, err
	}
	roots := git.DeclaredArtifactRoots(repoPath)
	moved, err := git.RelocatedIntoRoots(ctx, repoPath, roots)
	if err != nil {
		return false, err
	}
	paths, additions, foreground := planAutoWrite(tracked, untracked, roots, skip, moved, lstatSize(repoPath), limits.MaxFileSize)
	if foreground {
		return false, nil
	}

	reportRootAdditions(out, additions)
	if len(paths) == 0 {
		if len(additions) > 0 {
			_, _ = fmt.Fprintln(out, ui.Success("Nothing left to commit"))
		} else {
			_, _ = fmt.Fprintln(out, ui.Success(NothingStagedLine(ctx, repoPath, "")))
		}
		return true, nil
	}

	tree, err := git.CaptureTree(ctx, repoPath, paths)
	if err != nil {
		return false, err
	}
	// The worker only moves HEAD. The index has to already hold this tree,
	// or the landed commit shows up as a staged undo.
	finish, err := git.InstallTree(ctx, repoPath, tree)
	if err != nil {
		return false, err
	}
	if _, err := defercommit.EnqueueTree(ctx, campRoot, repoPath, tree, opts); err != nil {
		_ = finish(false)
		if errors.Is(err, git.ErrNoChanges) {
			_, _ = fmt.Fprintln(out, ui.Success("Nothing to commit"))
			return true, nil
		}
		_, _ = fmt.Fprintln(out, ui.Warning(fmt.Sprintf(
			"Could not queue this commit (%v); staging in the foreground.", err)))
		return false, nil
	}
	if err := finish(true); err != nil {
		return false, err
	}
	_, _ = fmt.Fprintln(out, ui.Success(DeferredCommitLine(len(paths))))
	_, _ = fmt.Fprintln(out, ui.Dim("  camp jobs   see it land"))
	return true, nil
}

// ReportDeclaredRootAdditions prints the tally for new files inside roots
// that were already declared before this commit, without expanding untracked
// directories. Roots declared during this same commit are reported by the
// guard instead, so passing them here would print the exclusion twice.
func ReportDeclaredRootAdditions(ctx context.Context, out io.Writer, repoPath string, roots []string) (bool, error) {
	if len(roots) == 0 {
		return false, nil
	}
	untracked, err := git.UntrackedCollapsed(ctx, repoPath)
	if err != nil {
		return false, err
	}
	_, additions, _ := planAutoWrite(nil, untracked, roots, nil, nil, lstatSize(repoPath), 0)
	reportRootAdditions(out, additions)
	return len(additions) > 0, nil
}

type rootAddition struct {
	root  string
	files int
	bytes int64
	dirs  int
}

func reportRootAdditions(out io.Writer, additions []rootAddition) {
	for _, addition := range additions {
		if addition.files > 0 {
			_, _ = fmt.Fprintf(out, "%s %s/ +%s files (%s) kept out of git\n",
				ui.SuccessIcon(), addition.root, ui.FormatCount(addition.files), ui.FormatBytes(addition.bytes))
			continue
		}
		if addition.dirs > 0 {
			_, _ = fmt.Fprintf(out, "%s %s/ kept out of git\n", ui.SuccessIcon(), addition.root)
		}
	}
}

// lstatSize reports the worktree size of a repo-relative path.
//
// Symlinks count. Their own length is the size, and the target is not
// followed. Only a missing path or a directory is skipped.
func lstatSize(repoPath string) func(rel string) (int64, bool) {
	return func(rel string) (int64, bool) {
		info, err := os.Lstat(filepath.Join(repoPath, filepath.FromSlash(strings.TrimSuffix(rel, "/"))))
		if err != nil || info.IsDir() {
			return 0, false
		}
		return info.Size(), true
	}
}

// ReportTrackedGrowthInRoots prints the threshold warning for a tracked file
// inside an already-declared root.
//
// The status walk skips those roots so it does not stat every untracked
// file there. A same-path edit is still committed, and it still has to be
// said out loud. diff --name-only finds it without that walk.
func ReportTrackedGrowthInRoots(ctx context.Context, out io.Writer, repoPath string, roots []string) error {
	if len(roots) == 0 {
		return nil
	}
	limits, err := stageguard.ResolveLimits(ctx, repoPath)
	if err != nil || limits.MaxFileSize <= 0 || limits.LargeFiles == stageguard.ModeOff {
		return err
	}
	changed, err := git.ChangedNames(ctx, repoPath)
	if err != nil {
		return err
	}
	sizeOf := lstatSize(repoPath)
	for _, path := range changed {
		if matchingRoot(path, roots) == "" {
			continue
		}
		size, ok := sizeOf(path)
		if !ok || size <= limits.MaxFileSize {
			continue
		}
		renderTrackedGrowth(out, stageguard.GuardViolation{
			Kind: stageguard.TrackedGrowth,
			Path: path,
			Size: size,
		}, limits)
	}
	return nil
}

// planAutoWrite decides whether an --auto-write commit can be captured
// without the full staging guard.
//
// sizeOf reports the worktree size of a repo-relative path. A false ok means
// the path is missing or a directory. forceInclude are moves of tracked
// content into a declared root; they are committed, not kept out.
func planAutoWrite(
	tracked, untracked, roots, skip, forceInclude []string,
	sizeOf func(rel string) (int64, bool),
	maxSize int64,
) (paths []string, additions []rootAddition, foreground bool) {
	byRoot := map[string]*rootAddition{}
	var order []string
	add := func(root string) *rootAddition {
		if existing, ok := byRoot[root]; ok {
			return existing
		}
		entry := &rootAddition{root: root}
		byRoot[root] = entry
		order = append(order, root)
		return entry
	}

	for _, path := range tracked {
		path = strings.TrimSuffix(path, "/")
		if path == "" || skipped(path, skip) {
			continue
		}
		// A file that grew past the threshold still has to be said out loud,
		// which the full guard does. Deletions have no size and stay here.
		if size, ok := sizeOf(path); ok && maxSize > 0 && size > maxSize {
			foreground = true
		}
		paths = append(paths, path)
	}

	for _, path := range untracked {
		if path == "" || skipped(strings.TrimSuffix(path, "/"), skip) {
			continue
		}
		if root := matchingRoot(strings.TrimSuffix(path, "/"), roots); root != "" {
			if !strings.HasSuffix(path, "/") && forcePath(path, forceInclude) {
				paths = append(paths, path)
				continue
			}
			entry := add(root)
			if strings.HasSuffix(path, "/") {
				entry.dirs++
				continue
			}
			size, ok := sizeOf(path)
			if !ok {
				continue
			}
			entry.files++
			entry.bytes += size
			continue
		}
		if strings.HasSuffix(path, "/") {
			// A collapsed untracked directory may hold a skill, a nested
			// repository, or a bulk drop. The full guard has to see it.
			// Ignored cache directories never appear in this list.
			// Keep scanning so a tally later in the list is not discarded.
			foreground = true
			continue
		}
		size, ok := sizeOf(path)
		if !ok {
			continue
		}
		if maxSize > 0 && size > maxSize {
			foreground = true
			continue
		}
		paths = append(paths, path)
	}

	additions = make([]rootAddition, 0, len(order))
	for _, root := range order {
		additions = append(additions, *byRoot[root])
	}
	return paths, additions, foreground
}

func forcePath(path string, include []string) bool {
	for _, candidate := range include {
		if candidate == path {
			return true
		}
	}
	return false
}

func matchingRoot(path string, roots []string) string {
	for _, root := range roots {
		root = strings.TrimSuffix(root, "/")
		if path == root || strings.HasPrefix(path, root+"/") {
			return root
		}
	}
	return ""
}

func skipped(path string, skip []string) bool {
	for _, prefix := range skip {
		prefix = strings.TrimSuffix(prefix, "/")
		if prefix == "" {
			continue
		}
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
