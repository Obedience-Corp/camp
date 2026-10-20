package cmdutil

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/defercommit"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/shellcmd"
	"github.com/Obedience-Corp/camp/internal/stageguard"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// lfsFilter drops the candidates git LFS manages. It is a parameter so a
// failing check can be exercised without a repository.
type lfsFilter func(ctx context.Context, repoPath string, candidates []stageguard.Candidate) ([]stageguard.Candidate, error)

// TryQueueAutoWriteBeforeStage queues an --auto-write commit without the
// foreground stage's status walk or a lock on the user's index.
//
// It returns true when the caller should stop. False means the ordinary
// foreground stage still has to run, because something might need to be
// said out loud or decided there: a first artifact root, a nested
// repository, a bulk drop, a tracked file past the threshold, or a merge in
// progress. A planning or queueing failure also falls back to the
// foreground, which reports it. Only cancellation is returned as an error.
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
	fallBack := func(err error) (bool, error) {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if err != nil {
			_, _ = fmt.Fprintln(out, ui.Warning(fmt.Sprintf(
				"Could not queue this commit (%v); staging in the foreground.", err)))
		}
		return false, nil
	}

	busy, err := git.OperationInProgress(ctx, repoPath)
	if err != nil || busy {
		return fallBack(err)
	}
	tracked, err := git.ChangedNames(ctx, repoPath)
	if err != nil {
		return fallBack(err)
	}
	untracked, err := git.UntrackedCollapsed(ctx, repoPath)
	if err != nil {
		return fallBack(err)
	}
	limits, err := stageguard.ResolveLimits(ctx, repoPath)
	if err != nil {
		return fallBack(nil)
	}
	roots := git.DeclaredArtifactRoots(repoPath)
	plan, err := git.PlanRootContent(ctx, repoPath, roots, &limits, false)
	if err != nil {
		return fallBack(err)
	}
	// block refuses a commit that keeps files out; the foreground says so.
	if limits.LargeFiles == stageguard.ModeBlock && len(plan.Kept) > 0 {
		return fallBack(nil)
	}
	paths, newFiles, ok := decideFastPath(ctx, repoPath, fastPathInputs{
		tracked:   tracked,
		untracked: untracked,
		roots:     roots,
		skip:      skip,
		plan:      plan,
		limits:    limits,
		sizeOf:    lstatSize(repoPath),
	}, stageguard.FilterLFSManaged)
	if !ok {
		return fallBack(nil)
	}
	if len(plan.Kept) > 0 {
		plan.Moved, err = git.PairMovesIntoRoots(ctx, repoPath, plan.Kept, deletedInWorktree(repoPath, tracked), newFiles)
		if err != nil {
			return fallBack(err)
		}
	}

	// The tally prints only once this path is certain to finish. A fallback
	// to the foreground stage reports it there instead.
	reportKept := func() {
		reportRootMoves(out, plan.Moved)
		reportRootAdditions(out, plan.Tally(roots))
	}
	if len(paths) == 0 {
		reportKept()
		if len(plan.Kept) > 0 || len(plan.KeptDirs) > 0 {
			_, _ = fmt.Fprintln(out, ui.Success("Nothing left to commit"))
		} else {
			_, _ = fmt.Fprintln(out, ui.Success(NothingStagedLine(ctx, repoPath, "")))
		}
		return true, nil
	}

	tree, err := git.CaptureTree(ctx, repoPath, paths, roots)
	if err != nil {
		return fallBack(err)
	}
	// The worker only moves HEAD. The index has to already hold this tree,
	// or the landed commit shows up as a staged undo.
	finish, err := git.InstallTree(ctx, repoPath, tree)
	if err != nil {
		return fallBack(err)
	}
	if _, err := defercommit.EnqueueTree(ctx, campRoot, repoPath, tree, opts); err != nil {
		if restoreErr := finish(false); restoreErr != nil {
			_, _ = fmt.Fprintln(out, ui.Warning(fmt.Sprintf("Could not restore the index: %v", restoreErr)))
		}
		if camperrors.Is(err, git.ErrNoChanges) {
			reportKept()
			_, _ = fmt.Fprintln(out, ui.Success("Nothing to commit"))
			return true, nil
		}
		return fallBack(err)
	}
	reportKept()
	// The job is durable from here, so a leftover backup is reported, not
	// treated as a failed commit.
	if err := finish(true); err != nil {
		_, _ = fmt.Fprintln(out, ui.Warning(fmt.Sprintf("Commit queued; %v", err)))
	}
	_, _ = fmt.Fprintln(out, ui.Success(DeferredCommitLine(len(paths))))
	_, _ = fmt.Fprintln(out, ui.Dim("  camp jobs   see it land"))
	return true, nil
}

// fastPathInputs is what the fast path read from the repository.
type fastPathInputs struct {
	tracked, untracked, roots, skip []string
	plan                            git.RootPlan
	limits                          stageguard.GuardLimits
	sizeOf                          func(rel string) (int64, bool)
}

// decideFastPath returns the paths to capture and the new files among them,
// or false when the commit has to go through the foreground stage.
//
// The foreground decides anything the guard would say out loud: a collapsed
// untracked directory, enough new files to trip the bulk guard, or a file
// over the threshold that commit.guards.allow and git LFS do not cover. A
// failed LFS check goes there too, where it is reported.
func decideFastPath(ctx context.Context, repoPath string, in fastPathInputs, lfs lfsFilter) ([]string, []string, bool) {
	paths, newFiles, foreground := planAutoWrite(in.tracked, in.untracked, in.roots, in.skip, in.plan, in.sizeOf)
	if foreground || bulkWouldTrip(len(newFiles), in.limits) {
		return nil, nil, false
	}
	loud, err := overThresholdToReport(ctx, repoPath, paths, in.sizeOf, in.limits, lfs)
	if err != nil || len(loud) > 0 {
		return nil, nil, false
	}
	return paths, newFiles, true
}

// deletedInWorktree returns the changed tracked paths that are gone from
// the worktree.
func deletedInWorktree(repoPath string, tracked []string) []string {
	var deleted []string
	for _, path := range tracked {
		if _, err := os.Lstat(filepath.Join(repoPath, filepath.FromSlash(path))); os.IsNotExist(err) {
			deleted = append(deleted, path)
		}
	}
	return deleted
}

// bulkWouldTrip reports whether added new files could trip the bulk guard.
// The guard counts files under one dominant directory; counting every new
// file is the conservative reading, so the fast path never queues a drop the
// foreground would refuse.
func bulkWouldTrip(added int, limits stageguard.GuardLimits) bool {
	return limits.Bulk != stageguard.ModeOff && limits.MaxAddedFiles > 0 && added >= limits.MaxAddedFiles
}

// ReportRootsKept prints what the stage kept out of declared artifact
// roots: each tracked file that moved into one past the size limit, then the
// media tally. It reports whether anything was kept.
func ReportRootsKept(out io.Writer, outcome *git.StageOutcome) bool {
	if outcome == nil || (len(outcome.RootsKept) == 0 && len(outcome.RootsMoved) == 0) {
		return false
	}
	reportRootMoves(out, outcome.RootsMoved)
	reportRootAdditions(out, outcome.RootsKept)
	return true
}

// reportRootMoves says that a tracked file left git's view by moving into
// a root, which is not the same thing as new media arriving there.
func reportRootMoves(out io.Writer, moves []git.RootMove) {
	for _, m := range moves {
		_, _ = fmt.Fprintf(out, "%s %s was tracked and is now %s (%s, over the size limit)\n",
			ui.WarningIcon(), m.From, m.To, ui.FormatBytes(m.Size))
		_, _ = fmt.Fprintf(out, "  git records %s as deleted, and the new path is kept out of git with the root's media.\n", m.From)
		_, _ = fmt.Fprintf(out, "  Keep it tracked:  git add -- %s\n", quotePath(m.To))
	}
}

// quotePath quotes a path for a copy-paste command, in the shell this
// platform's users have, when it holds anything a shell would interpret.
func quotePath(path string) string {
	if strings.IndexFunc(path, needsShellQuote) < 0 {
		return path
	}
	return shellcmd.QuoteLiteral(path)
}

func needsShellQuote(r rune) bool {
	switch {
	case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		return false
	case r == '/', r == '.', r == '-', r == '_':
		return false
	}
	return true
}

func reportRootAdditions(out io.Writer, tallies []git.RootTally) {
	for _, tally := range tallies {
		if tally.Files > 0 {
			_, _ = fmt.Fprintf(out, "%s %s/ +%s files (%s) kept out of git\n",
				ui.SuccessIcon(), tally.Root, ui.FormatCount(tally.Files), ui.FormatBytes(tally.Bytes))
			_, _ = fmt.Fprintln(out, ui.Dim("  Commit them anyway with --commit-large"))
		}
		for _, dir := range tally.NestedRepos {
			_, _ = fmt.Fprintf(out, "%s %s/ is a nested repository, kept out of git\n", ui.SuccessIcon(), dir)
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
// The status walk skips those roots, and the root plan only sizes untracked
// files. A same-path edit is still committed, and it still has to be said
// out loud. diff --name-only finds it without that walk. commitLarge
// silences it, as it silences the walk.
func ReportTrackedGrowthInRoots(ctx context.Context, out io.Writer, repoPath string, roots []string, commitLarge bool) error {
	if len(roots) == 0 || commitLarge {
		return nil
	}
	limits, err := stageguard.ResolveLimits(ctx, repoPath)
	if err != nil {
		return err
	}
	changed, err := git.ChangedNames(ctx, repoPath)
	if err != nil {
		return err
	}
	var inRoots []string
	for _, path := range changed {
		if matchingRoot(path, roots) != "" {
			inRoots = append(inRoots, path)
		}
	}
	loud, err := overThresholdToReport(ctx, repoPath, inRoots, lstatSize(repoPath), limits, stageguard.FilterLFSManaged)
	if err != nil {
		return err
	}
	for _, candidate := range loud {
		renderTrackedGrowth(out, stageguard.GuardViolation{
			Kind: stageguard.TrackedGrowth,
			Path: candidate.Path,
			Size: candidate.Size,
		}, limits)
	}
	return nil
}

// overThresholdToReport returns the paths the staging guard would still say
// something about: larger than MaxFileSize, not matched by
// commit.guards.allow, and not managed by git LFS. The status walk filters
// the same way before it reports, so a path it keeps quiet stays quiet here.
func overThresholdToReport(
	ctx context.Context,
	repoPath string,
	paths []string,
	sizeOf func(rel string) (int64, bool),
	limits stageguard.GuardLimits,
	lfs lfsFilter,
) ([]stageguard.Candidate, error) {
	over := overThreshold(paths, sizeOf, limits)
	if len(over) == 0 {
		return nil, nil
	}
	return lfs(ctx, repoPath, over)
}

func overThreshold(paths []string, sizeOf func(rel string) (int64, bool), limits stageguard.GuardLimits) []stageguard.Candidate {
	if limits.MaxFileSize <= 0 || limits.LargeFiles == stageguard.ModeOff {
		return nil
	}
	var over []stageguard.Candidate
	for _, path := range paths {
		size, ok := sizeOf(path)
		if !ok || size <= limits.MaxFileSize || stageguard.MatchesAllow(path, limits.Allow) {
			continue
		}
		over = append(over, stageguard.Candidate{Path: path, Size: size})
	}
	return over
}

// planAutoWrite lists the paths an --auto-write commit captures and the new
// files among them, or reports that the foreground stage has to run.
//
// Untracked content inside declared roots comes from plan, which has sized
// every file there; entries for it in the collapsed listing are ignored.
// Outside the roots a collapsed untracked directory goes to the foreground,
// because it may hold a skill, a nested repository, or a bulk drop.
func planAutoWrite(
	tracked, untracked, roots, skip []string,
	plan git.RootPlan,
	sizeOf func(rel string) (int64, bool),
) (paths, newFiles []string, foreground bool) {
	seen := map[string]struct{}{}
	include := func(path string) bool {
		if _, ok := seen[path]; ok {
			return false
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
		return true
	}

	for _, path := range tracked {
		path = strings.TrimSuffix(path, "/")
		if path == "" || skipped(path, skip) {
			continue
		}
		include(path)
	}
	for _, path := range append(plan.CommitPaths(), plan.Links...) {
		if skipped(path, skip) {
			continue
		}
		if include(path) {
			newFiles = append(newFiles, path)
		}
	}
	for _, path := range untracked {
		trimmed := strings.TrimSuffix(path, "/")
		if trimmed == "" || skipped(trimmed, skip) || matchingRoot(trimmed, roots) != "" {
			continue
		}
		if strings.HasSuffix(path, "/") {
			// Ignored cache directories never appear in this list.
			// Keep scanning so a later entry is still classified.
			foreground = true
			continue
		}
		if _, ok := sizeOf(path); !ok {
			continue
		}
		if include(path) {
			newFiles = append(newFiles, path)
		}
	}
	return paths, newFiles, foreground
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
