package cmdutil

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/stageguard"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// What the user sees when the staging guard acts. The guard itself returns
// typed data and no prose; all copy lives here, so camp's wording and fest's
// can differ without either reimplementing the decision.
//
// Two rules shape every message below. Camp acting automatically is only safe
// if it says what it did, so nothing here is silent. And every line announcing
// new behavior carries its own undo or off switch, so a user who disagrees
// with camp is one command from reversing it rather than searching docs.

// GitignoreRuleComment explains, in the file itself, why an artifact root's
// ignore rule is there.
const GitignoreRuleComment = "# Camp artifact root (synced with 'camp sync', not git)"

// GuardHandling is the result of acting on a staging outcome, kept as data so
// `camp commit --json` renders the same facts the human output describes.
type GuardHandling struct {
	// Declared holds roots camp declared during this commit.
	Declared []DeclaredRoot
	// Refused holds files camp excluded but would not choose a root for.
	Refused []RefusedFile
	// ProjectExcluded holds files excluded inside a project, where camp never
	// declares anything.
	ProjectExcluded []RefusedFile
	// TrackedGrowth holds tracked files reported but always committed.
	TrackedGrowth []stageguard.GuardViolation
}

// DeclaredRoot records one artifact root camp declared, or reused.
type DeclaredRoot struct {
	Root string
	// Files are the over-threshold paths that landed under this root.
	Files []stageguard.GuardViolation
	// Bytes is the total size kept out of git for this root.
	Bytes int64
	// GitignoreWritten is true when camp also added the ignore rule, which
	// only happens for a clean root.
	GitignoreWritten bool
	// KeptWithGit names the files under this root that were still staged, so
	// the report can say what git still owns.
	KeptWithGit string
	// Existing is true when the root was already declared and camp reused it.
	Existing bool
}

// RefusedFile is a file camp excluded without declaring anything.
type RefusedFile struct {
	Violation stageguard.GuardViolation
	// Reason is the machine-readable cause, surfaced verbatim in --json.
	Reason string
	// Refusal is the specific boundary that was hit, so the report can explain
	// which rule applied rather than stating a generic refusal.
	Refusal artifacts.RefusalReason
}

// Reasons reported in --json, matching the design's vocabulary.
const (
	reasonNeedsRootDecision = "needs_root_decision"
	reasonProjectLargeFile  = "project_large_file"
)

// handleStageOutcome acts on the guard's decision and reports it.
//
// At the campaign root it declares artifact roots for excluded files and
// stages the resulting bookkeeping into the same commit, so the commit that
// excluded the footage also carries the declaration explaining why. Inside a
// project it declares nothing: an artifact root there would keep the bytes off
// the project's remote, and camp cannot know whether collaborators need them.
func HandleStageOutcome(
	ctx context.Context,
	out io.Writer,
	campRoot, repoPath string,
	outcome *git.StageOutcome,
) (*GuardHandling, error) {
	if outcome.Empty() {
		return nil, nil
	}

	handling := &GuardHandling{TrackedGrowth: outcome.Reported}

	if outcome.Limits.ScopeProject {
		for _, v := range outcome.Excluded {
			handling.ProjectExcluded = append(handling.ProjectExcluded,
				RefusedFile{Violation: v, Reason: reasonProjectLargeFile})
		}
	} else if err := declareRootsForExclusions(ctx, campRoot, outcome, handling); err != nil {
		return nil, err
	}

	renderGuardHandling(out, campRoot, repoPath, handling, outcome.Limits)
	return handling, nil
}

// declareRootsForExclusions resolves and writes an artifact root per excluded
// file, grouping files that share one root.
func declareRootsForExclusions(
	ctx context.Context,
	campRoot string,
	outcome *git.StageOutcome,
	handling *GuardHandling,
) error {
	cfg, err := artifacts.Load(campRoot)
	if err != nil {
		return err
	}

	byRoot := make(map[string]*DeclaredRoot)
	var order []string

	for _, v := range outcome.Excluded {
		resolved := artifacts.ResolveAutoRoot(cfg, campRoot, v.Path)
		if resolved.Refusal != artifacts.RefusalNone {
			handling.Refused = append(handling.Refused,
				RefusedFile{Violation: v, Reason: reasonNeedsRootDecision, Refusal: resolved.Refusal})
			continue
		}

		entry, seen := byRoot[resolved.Path]
		if !seen {
			entry = &DeclaredRoot{Root: resolved.Path, Existing: resolved.Existing}
			byRoot[resolved.Path] = entry
			order = append(order, resolved.Path)

			if resolved.Declared() {
				if addErr := cfg.Add(artifacts.Root{Path: resolved.Path}); addErr != nil {
					return camperrors.Wrapf(addErr, "declare artifact root %s", resolved.Path)
				}
			}
		}
		entry.Files = append(entry.Files, v)
		entry.Bytes += v.Size
	}

	if len(order) == 0 {
		return nil
	}
	if err := cfg.Save(campRoot); err != nil {
		return err
	}

	// A clean root gets its ignore rule, which makes everything landing there
	// later artifact content by construction. A mixed root never does: the
	// rule would hide its tracked files from git without untracking them.
	for _, root := range order {
		entry := byRoot[root]
		if entry.Existing {
			continue
		}
		wrote, err := writeCleanRootIgnoreRule(ctx, campRoot, root)
		if err != nil {
			return err
		}
		entry.GitignoreWritten = wrote
	}

	// Ask git what is still staged under each root, so the report names the
	// files that remain git's rather than guessing from the exclusion list.
	for _, root := range order {
		byRoot[root].KeptWithGit = stagedNamesUnder(ctx, campRoot, root)
	}

	handling.Declared = append(handling.Declared, collectRoots(order, byRoot)...)
	return stageGuardBookkeeping(ctx, campRoot, handling)
}

func collectRoots(order []string, byRoot map[string]*DeclaredRoot) []DeclaredRoot {
	roots := make([]DeclaredRoot, 0, len(order))
	for _, root := range order {
		roots = append(roots, *byRoot[root])
	}
	return roots
}

// stagedNamesUnder renders the staged file names beneath a root as a human
// list ("script.md and notes.md"), or "" when nothing under it is staged.
func stagedNamesUnder(ctx context.Context, campRoot, root string) string {
	paths, err := git.StagedPathsUnder(ctx, campRoot, root)
	if err != nil || len(paths) == 0 {
		return ""
	}
	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, filepath.Base(p))
	}
	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return fmt.Sprintf("%s and %s more", names[0], ui.FormatCount(len(names)-1))
	}
}

// writeCleanRootIgnoreRule adds the ignore rule when the root holds no tracked
// content, reporting whether it wrote.
func writeCleanRootIgnoreRule(ctx context.Context, campRoot, root string) (bool, error) {
	if artifacts.HasTrackedFiles(ctx, campRoot, root) {
		return false, nil
	}
	if artifacts.IsGitignored(ctx, campRoot, root) {
		return false, nil
	}
	wrote, err := fsutil.AppendGitignoreEntryIfMissing(
		filepath.Join(campRoot, ".gitignore"), root+"/", GitignoreRuleComment)
	if err != nil {
		return false, camperrors.Wrapf(err, "add %q to .gitignore", root+"/")
	}
	return wrote, nil
}

// stageGuardBookkeeping stages the declaration file and .gitignore into the
// commit that excluded the bytes, so the commit carries its own explanation
// rather than leaving the declaration for a later, unrelated commit.
//
// manifests: phase 002 adds committed artifact manifests here.
func stageGuardBookkeeping(ctx context.Context, campRoot string, handling *GuardHandling) error {
	paths := []string{artifacts.ConfigRelPath}
	for _, d := range handling.Declared {
		if d.GitignoreWritten {
			paths = append(paths, ".gitignore")
			break
		}
	}
	// Explicit paths, so this staging call is not itself guarded.
	return git.StageFiles(ctx, campRoot, paths...)
}

// renderGuardHandling writes the human report.
func renderGuardHandling(
	out io.Writer,
	campRoot, repoPath string,
	handling *GuardHandling,
	limits stageguard.GuardLimits,
) {
	for _, d := range handling.Declared {
		renderDeclaredRoot(out, campRoot, d)
	}
	for _, r := range handling.Refused {
		renderRootRefusal(out, r)
	}
	for _, p := range handling.ProjectExcluded {
		renderProjectExclusion(out, p)
	}
	for _, t := range handling.TrackedGrowth {
		renderTrackedGrowth(out, t, limits)
	}
}

// renderDeclaredRoot reports a root camp declared or reused.
//
// Reporting is proportionate: a newly declared root gets the full block that
// teaches what an artifact root is, because it is new behavior the user has
// not seen. Adding files to a root they already declared gets one tally line,
// because they already know.
func renderDeclaredRoot(out io.Writer, campRoot string, d DeclaredRoot) {
	if d.Existing {
		_, _ = fmt.Fprintf(out, "%s %s/ +%s files (%s) kept out of git\n",
			ui.SuccessIcon(), d.Root, ui.FormatCount(len(d.Files)), ui.FormatBytes(d.Bytes))
		return
	}

	names := violationNames(d.Files)
	_, _ = fmt.Fprintf(out, "%s %s/ is now an artifact root (%s, %s)\n",
		ui.SuccessIcon(), d.Root, names, ui.FormatBytes(d.Bytes))
	_, _ = fmt.Fprintf(out, "  Kept out of git, syncs with 'camp sync --from <machine>'\n")

	// Name what still belongs to git. Without this the user has just been told
	// their directory is now "artifact content" and has no way to know the
	// notes they wrote beside the footage are still versioned.
	if kept := d.KeptWithGit; kept != "" {
		_, _ = fmt.Fprintf(out, "  %s stay with git\n", kept)
	}
	_, _ = fmt.Fprintf(out, "  Undo: camp artifacts remove %s\n", d.Root)

	if siblings := siblingSuggestion(campRoot, d.Root); siblings != "" {
		_, _ = fmt.Fprint(out, siblings)
	}
}

// siblingSuggestion offers to collapse accumulated sibling roots. Camp
// suggests and never collapses: merging roots changes what syncs, and only the
// user knows whether that is wanted.
func siblingSuggestion(campRoot, root string) string {
	cfg, err := artifacts.Load(campRoot)
	if err != nil {
		return ""
	}
	parent := filepath.ToSlash(filepath.Dir(root))
	if parent == "." || parent == "" {
		return ""
	}
	if n := artifacts.SiblingRootCount(cfg, parent); n >= 3 {
		return fmt.Sprintf("  %d artifact roots now under %s/. Collapse with: camp artifacts add %s\n",
			n, parent, parent)
	}
	return ""
}

// renderRootRefusal reports a file camp excluded but would not choose a root
// for. The commit still succeeded; only this file was left out.
func renderRootRefusal(out io.Writer, r RefusedFile) {
	v := r.Violation
	_, _ = fmt.Fprintf(out, "%s %s %s %s\n",
		ui.WarningIcon(), ui.FormatBytes(v.Size), v.Path, refusalExplanation(r.Refusal))
	_, _ = fmt.Fprintf(out, "  It was left out of this commit.\n")
	_, _ = fmt.Fprintf(out, "    camp artifacts add <a directory you choose>   or gitignore it\n")
}

// refusalExplanation says why camp declined, in the terms of the specific
// boundary that was hit. A generic "camp will not create a root here" leaves
// the user guessing which rule applied and therefore what to do instead.
func refusalExplanation(reason artifacts.RefusalReason) string {
	switch reason {
	case artifacts.RefusalCampaignRoot:
		return "sits at the campaign root; camp will not make the campaign an artifact root."
	case artifacts.RefusalCampaignState:
		return "lives under .campaign/; camp will not make its own state tree an artifact root."
	case artifacts.RefusalRepoRoot:
		return "sits at a git repository root; an artifact root there would cross a repo boundary."
	default:
		return "sits where camp will not create an artifact root."
	}
}

// renderProjectExclusion reports a large file inside a project, where camp
// declares nothing and says what it cannot decide.
func renderProjectExclusion(out io.Writer, r RefusedFile) {
	v := r.Violation
	_, _ = fmt.Fprintf(out, "%s %s (%s) was left out of this commit\n",
		ui.WarningIcon(), v.Path, ui.FormatBytes(v.Size))
	_, _ = fmt.Fprintf(out, "  Camp does not auto-manage large files inside a project: an artifact root\n")
	_, _ = fmt.Fprintf(out, "  would keep it off the project's remote, so collaborators would not get it.\n")
	_, _ = fmt.Fprintf(out, "    if it is build output   echo '%s' >> .gitignore\n", v.Path)
	_, _ = fmt.Fprintf(out, "    if others need it       git lfs track '%s'\n", lfsPattern(v.Path))
	_, _ = fmt.Fprintf(out, "    if it belongs in git    add to commit.guards.allow, or --commit-large\n")
}

// renderTrackedGrowth reports a tracked file that crossed the threshold. It is
// always committed: excluding it would leave the stale blob in git while the
// new bytes on disk belonged to nothing.
func renderTrackedGrowth(out io.Writer, v stageguard.GuardViolation, limits stageguard.GuardLimits) {
	_, _ = fmt.Fprintf(out, "%s %s is tracked and now %s (limit %s)\n",
		ui.WarningIcon(), v.Path, ui.FormatBytes(v.Size), ui.FormatBytes(limits.MaxFileSize))
	_, _ = fmt.Fprintf(out, "  Tracked files are always committed. To stop tracking it:\n")
	_, _ = fmt.Fprintf(out, "    git rm --cached %s\n", v.Path)
	_, _ = fmt.Fprintf(out, "    camp artifacts add %s\n", filepath.ToSlash(filepath.Dir(v.Path)))
	_, _ = fmt.Fprintf(out, "  Or allow it: commit.guards.allow in .campaign/campaign.yaml\n")
}

// violationNames renders the file names behind a root, abbreviating past two
// so a root absorbing hundreds of files does not print hundreds of names.
func violationNames(violations []stageguard.GuardViolation) string {
	switch len(violations) {
	case 0:
		return ""
	case 1:
		return filepath.Base(violations[0].Path)
	case 2:
		return filepath.Base(violations[0].Path) + ", " + filepath.Base(violations[1].Path)
	default:
		return fmt.Sprintf("%s and %s more",
			filepath.Base(violations[0].Path), ui.FormatCount(len(violations)-1))
	}
}

// lfsPattern turns a path into the glob a user would most likely want to track,
// keeping the directory and widening only the file name to its extension.
func lfsPattern(path string) string {
	dir := filepath.ToSlash(filepath.Dir(path))
	ext := filepath.Ext(path)
	if ext == "" {
		return path
	}
	if dir == "." || dir == "" {
		return "*" + ext
	}
	return dir + "/*" + ext
}

// GuardExcludedEverything reports whether the guard kept files out and nothing
// remains staged.
//
// The unstaged-changes check that normally decides "is there anything to
// commit" answers yes here, because the excluded file is still sitting in the
// worktree as an unstaged change. Without this, camp reports what it kept out
// and then fails with git's "nothing to commit", which reads as camp having
// broken rather than having done exactly what it said.
func GuardExcludedEverything(ctx context.Context, repoPath string, handling *GuardHandling) (bool, error) {
	if handling == nil || !handling.ExcludedAnything() {
		return false, nil
	}
	staged, err := git.HasStagedChanges(ctx, repoPath)
	if err != nil {
		return false, err
	}
	return !staged, nil
}

// ExcludedAnything reports whether the guard kept any file out of the index.
// Tracked growth does not count: those files are always committed.
func (h *GuardHandling) ExcludedAnything() bool {
	if h == nil {
		return false
	}
	if len(h.Refused) > 0 || len(h.ProjectExcluded) > 0 {
		return true
	}
	for _, d := range h.Declared {
		if len(d.Files) > 0 {
			return true
		}
	}
	return false
}

// ReportNothingLeftToCommit tells the user the commit was a no-op because
// everything they changed was kept out of git, and exits the caller cleanly.
func ReportNothingLeftToCommit(out io.Writer) {
	_, _ = fmt.Fprintf(out, "%s Nothing left to commit: everything changed was kept out of git\n", ui.SuccessIcon())
}
