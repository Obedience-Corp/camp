package workitem

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

const (
	linkRegistryRelPath = ".campaign/workitems/links.yaml"

	skipReasonOutOfScope          = "(out of scope)"
	skipReasonExcludeFlag         = "(--exclude)"
	skipReasonPointerOffByDefault = "(submodule pointer; use --include-submodule-pointer)"
)

// PlanContext labels which staging-matrix row a plan came from. Stable
// strings: used by both the human plan output and the integration tests.
type PlanContext string

const (
	PlanContextWorkitemDir   PlanContext = "workitem directory"
	PlanContextCampaignRoot  PlanContext = "campaign root"
	PlanContextLinkedProject PlanContext = "linked project"
	PlanContextFestival      PlanContext = "festival"
	PlanContextStagedOnly    PlanContext = "staged-only"
)

// PlanOptions inputs to ComputePlan. Mirrors the camp workitem commit flag
// surface so the planner can be exercised from tests without spinning up cobra.
type PlanOptions struct {
	Cwd                     string
	Explicit                string   // --workitem <selector>
	Project                 string   // --project <name> override
	Includes                []string // --include <path> (repeatable)
	Excludes                []string // --exclude <path> (repeatable)
	StagedOnly              bool     // --staged
	IncludeSubmodulePointer bool     // --include-submodule-pointer
	CampaignID              string
}

// SkippedEntry pairs a path with a stable reason string.
type SkippedEntry struct {
	Path   string
	Reason string
}

// StagingPlan is the contract between ComputePlan and the commit runner. Stage
// is what the planner intends to `git add`. PreStaged is what is already in
// the index (used by --staged so we do not re-stage). Skip explains paths
// intentionally left out.
type StagingPlan struct {
	Workitem    *wkitem.WorkItem
	WorkitemRef string
	QuestID     string
	Context     PlanContext
	ContextNote string
	RepoRoot    string
	Stage       []string
	PreStaged   []string
	Skip        []SkippedEntry
	Tag         string
}

// ComputePlan resolves a workitem from the current context, branches on the
// matrix row that applies, and returns a StagingPlan that the commit runner
// can hand to commit.Workitem. Refusal modes (no workitem, empty plan with no
// override) surface as typed errors so the CLI can map them to exit codes.
func ComputePlan(ctx context.Context, campaignRoot string, opts PlanOptions) (*StagingPlan, error) {
	if campaignRoot == "" {
		return nil, camperrors.NewValidation("root", "campaign root required", nil)
	}

	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Explicit: opts.Explicit,
		Cwd:      opts.Cwd,
	})
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve workitem")
	}
	if res == nil || res.Workitem == nil {
		return nil, ErrNoWorkitemContext
	}

	wi := res.Workitem
	ref := refOf(wi)
	plan := &StagingPlan{
		Workitem:    wi,
		WorkitemRef: ref,
		QuestID:     res.QuestID,
		Tag:         commitkit.FormatContextTagsFull(opts.CampaignID, res.QuestID, "", ref),
	}

	if opts.StagedOnly {
		plan.Context = PlanContextStagedOnly
		plan.ContextNote = "using current git index"
		plan.RepoRoot = campaignRoot
		staged, err := listStagedFiles(ctx, campaignRoot)
		if err != nil {
			return nil, camperrors.Wrap(err, "list staged files")
		}
		plan.PreStaged = staged
		// Includes still apply on top of --staged so the user can add a tag-only
		// commit of additional explicit paths.
		stage, skip, err := applyIncludes(campaignRoot, nil, opts.Includes)
		if err != nil {
			return nil, err
		}
		plan.Stage = applyExcludes(stage, opts.Excludes, &plan.Skip)
		plan.Skip = append(plan.Skip, skip...)
		return plan, nil
	}

	if opts.Project != "" {
		return computeProjectPlan(ctx, campaignRoot, opts, plan)
	}

	switch res.Source {
	case resolver.SourceLink:
		return computeLinkPlan(ctx, campaignRoot, opts, plan)
	case resolver.SourceFestival:
		return computeFestivalPlan(ctx, campaignRoot, opts, plan)
	default:
		// SourceExplicit, SourceAncestor, SourceCurrent — all stage from the
		// campaign root scoped to the workitem directory.
		return computeWorkitemDirPlan(ctx, campaignRoot, opts, plan, res.Source)
	}
}

func computeWorkitemDirPlan(ctx context.Context, root string, opts PlanOptions, plan *StagingPlan, src resolver.Source) (*StagingPlan, error) {
	plan.RepoRoot = root
	plan.Context = PlanContextWorkitemDir
	if src != resolver.SourceAncestor {
		plan.Context = PlanContextCampaignRoot
	}
	plan.ContextNote = workitemDirNote(plan.Workitem, src)

	stage, err := listChangedFilesUnder(ctx, root, plan.Workitem.RelativePath)
	if err != nil {
		return nil, camperrors.Wrap(err, "list workitem changes")
	}

	if dirty, err := pathIsDirty(ctx, root, linkRegistryRelPath); err == nil && dirty {
		stage = append(stage, linkRegistryRelPath)
	}

	if opts.IncludeSubmodulePointer {
		if pointers, perr := listDirtySubmodulePointers(ctx, root); perr == nil {
			stage = append(stage, pointers...)
		}
	}

	added, addSkip, err := applyIncludes(root, stage, opts.Includes)
	if err != nil {
		return nil, err
	}
	plan.Skip = append(plan.Skip, addSkip...)
	plan.Stage = applyExcludes(added, opts.Excludes, &plan.Skip)

	plan.Skip = append(plan.Skip, listSubmodulePointerSkips(ctx, root, opts.IncludeSubmodulePointer)...)

	plan.Stage = dedupeSorted(plan.Stage)
	return plan, nil
}

func computeLinkPlan(ctx context.Context, root string, opts PlanOptions, plan *StagingPlan) (*StagingPlan, error) {
	scopePath, err := pickPrimaryProjectScopePath(ctx, root, plan.Workitem)
	if err != nil {
		return nil, err
	}
	repoRoot := filepath.Join(root, filepath.FromSlash(scopePath))
	plan.RepoRoot = repoRoot
	plan.Context = PlanContextLinkedProject
	plan.ContextNote = "linked project " + filepath.Base(scopePath)

	stage, err := listChangedFilesUnder(ctx, repoRoot, "")
	if err != nil {
		return nil, camperrors.Wrap(err, "list project changes")
	}

	added, addSkip, err := applyIncludes(repoRoot, stage, opts.Includes)
	if err != nil {
		return nil, err
	}
	plan.Skip = append(plan.Skip, addSkip...)
	plan.Stage = applyExcludes(added, opts.Excludes, &plan.Skip)
	plan.Stage = dedupeSorted(plan.Stage)
	return plan, nil
}

func computeFestivalPlan(ctx context.Context, root string, opts PlanOptions, plan *StagingPlan) (*StagingPlan, error) {
	plan.RepoRoot = root
	plan.Context = PlanContextFestival
	scope := primaryFestivalScopePath(ctx, root, plan.Workitem)
	if scope == "" {
		scope = plan.Workitem.RelativePath
	}
	plan.ContextNote = "festival " + filepath.Base(scope)

	stage, err := listChangedFilesUnder(ctx, root, scope)
	if err != nil {
		return nil, camperrors.Wrap(err, "list festival changes")
	}
	if dirty, err := pathIsDirty(ctx, root, linkRegistryRelPath); err == nil && dirty {
		stage = append(stage, linkRegistryRelPath)
	}
	added, addSkip, err := applyIncludes(root, stage, opts.Includes)
	if err != nil {
		return nil, err
	}
	plan.Skip = append(plan.Skip, addSkip...)
	plan.Stage = applyExcludes(added, opts.Excludes, &plan.Skip)
	plan.Stage = dedupeSorted(plan.Stage)
	return plan, nil
}

func computeProjectPlan(ctx context.Context, root string, opts PlanOptions, plan *StagingPlan) (*StagingPlan, error) {
	repoRoot := filepath.Join(root, "projects", opts.Project)
	plan.RepoRoot = repoRoot
	plan.Context = PlanContextLinkedProject
	plan.ContextNote = "project " + opts.Project + " (--project override)"

	stage, err := listChangedFilesUnder(ctx, repoRoot, "")
	if err != nil {
		return nil, camperrors.Wrap(err, "list project changes")
	}
	added, addSkip, err := applyIncludes(repoRoot, stage, opts.Includes)
	if err != nil {
		return nil, err
	}
	plan.Skip = append(plan.Skip, addSkip...)
	plan.Stage = applyExcludes(added, opts.Excludes, &plan.Skip)
	plan.Stage = dedupeSorted(plan.Stage)
	return plan, nil
}

// PrintPlan writes the human-readable plan summary to w. Mirrors
// COMMIT_DESIGN.md §6 exactly so the integration tests can grep stable lines.
func PrintPlan(w io.Writer, plan *StagingPlan) error {
	if plan == nil || plan.Workitem == nil {
		return camperrors.NewValidation("plan", "nil plan", nil)
	}
	fmt.Fprintf(w, "workitem: %s (ref: %s)\n", plan.Workitem.StableID, plan.WorkitemRef)
	fmt.Fprintf(w, "context:  %s", plan.Context)
	if plan.ContextNote != "" {
		fmt.Fprintf(w, " (%s)", plan.ContextNote)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "staging:")
	if len(plan.PreStaged) > 0 {
		for _, p := range plan.PreStaged {
			fmt.Fprintf(w, "  S  %s\n", p)
		}
	}
	for _, p := range plan.Stage {
		fmt.Fprintf(w, "  A  %s\n", p)
	}
	if len(plan.Skip) > 0 {
		fmt.Fprintln(w, "skipped:")
		for _, s := range plan.Skip {
			fmt.Fprintf(w, "  %s %s\n", s.Path, s.Reason)
		}
	}
	fmt.Fprintf(w, "tag:    %s\n", plan.Tag)
	return nil
}

// refOf reads the workitem ref off SourceMetadata (populated by
// workitem.ApplyMetadata in sequence 03 task 02).
func refOf(wi *wkitem.WorkItem) string {
	if wi == nil || wi.SourceMetadata == nil {
		return ""
	}
	if v, ok := wi.SourceMetadata["ref"].(string); ok {
		return v
	}
	return ""
}

func workitemDirNote(wi *wkitem.WorkItem, src resolver.Source) string {
	switch src {
	case resolver.SourceAncestor:
		return wi.RelativePath
	case resolver.SourceExplicit:
		return "explicit --workitem"
	case resolver.SourceCurrent:
		return "via current.yaml"
	default:
		return string(src)
	}
}

// pickPrimaryProjectScopePath finds the primary path link scope (project,
// repo, or worktree) that points at the workitem. Returns the campaign-
// relative path so callers can join with the campaign root to get the
// staging repo root.
func pickPrimaryProjectScopePath(ctx context.Context, root string, wi *wkitem.WorkItem) (string, error) {
	registry, err := links.Load(ctx, root)
	if err != nil {
		return "", camperrors.Wrap(err, "load links")
	}
	for i := range registry.Links {
		link := &registry.Links[i]
		if link.Role != links.RolePrimary {
			continue
		}
		if link.WorkitemID != wi.StableID && link.WorkitemID != wi.Key {
			continue
		}
		switch link.Scope.Kind {
		case links.ScopeProject, links.ScopeRepo, links.ScopeWorktree:
			return link.Scope.Path, nil
		}
	}
	return "", camperrors.NewValidation("link",
		"no primary project link points at workitem "+wi.StableID, nil)
}

func primaryFestivalScopePath(ctx context.Context, root string, wi *wkitem.WorkItem) string {
	registry, err := links.Load(ctx, root)
	if err != nil || registry == nil {
		return ""
	}
	for i := range registry.Links {
		link := &registry.Links[i]
		if link.Role != links.RolePrimary || link.Scope.Kind != links.ScopeFestival {
			continue
		}
		if link.WorkitemID != wi.StableID && link.WorkitemID != wi.Key {
			continue
		}
		return link.Scope.Path
	}
	return ""
}

// listChangedFilesUnder returns repo-relative paths for changes inside prefix
// (or the entire repo when prefix is empty). Wraps `git status --porcelain`.
func listChangedFilesUnder(ctx context.Context, repoRoot, prefix string) ([]string, error) {
	args := []string{"-C", repoRoot, "status", "--porcelain"}
	if prefix != "" {
		args = append(args, "--", prefix)
	}
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		// Rename entries look like "old -> new"; take the new path.
		if i := strings.Index(path, " -> "); i >= 0 {
			path = path[i+4:]
		}
		files = append(files, path)
	}
	return files, nil
}

func listStagedFiles(ctx context.Context, repoRoot string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "diff", "--cached", "--name-only").Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		files = append(files, line)
	}
	return files, nil
}

func pathIsDirty(ctx context.Context, repoRoot, relPath string) (bool, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "status", "--porcelain", "--", relPath).Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func listDirtySubmodulePointers(ctx context.Context, repoRoot string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", repoRoot, "status", "--porcelain", "--", "projects").Output()
	if err != nil {
		return nil, err
	}
	var pointers []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if strings.HasPrefix(path, "projects/") {
			pointers = append(pointers, path)
		}
	}
	return pointers, nil
}

func listSubmodulePointerSkips(ctx context.Context, root string, allowed bool) []SkippedEntry {
	if allowed {
		return nil
	}
	pointers, err := listDirtySubmodulePointers(ctx, root)
	if err != nil {
		return nil
	}
	out := make([]SkippedEntry, 0, len(pointers))
	for _, p := range pointers {
		out = append(out, SkippedEntry{Path: p, Reason: skipReasonPointerOffByDefault})
	}
	return out
}

func applyIncludes(repoRoot string, stage, includes []string) ([]string, []SkippedEntry, error) {
	if len(includes) == 0 {
		return stage, nil, nil
	}
	out := append([]string{}, stage...)
	var skip []SkippedEntry
	for _, inc := range includes {
		rel, err := relativeToRepo(repoRoot, inc)
		if err != nil {
			skip = append(skip, SkippedEntry{Path: inc, Reason: skipReasonOutOfScope})
			continue
		}
		out = append(out, rel)
	}
	return out, skip, nil
}

func applyExcludes(stage []string, excludes []string, skip *[]SkippedEntry) []string {
	if len(excludes) == 0 {
		return stage
	}
	ex := make(map[string]bool, len(excludes))
	for _, e := range excludes {
		ex[filepath.ToSlash(e)] = true
	}
	kept := stage[:0]
	for _, p := range stage {
		if ex[filepath.ToSlash(p)] {
			*skip = append(*skip, SkippedEntry{Path: p, Reason: skipReasonExcludeFlag})
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

func relativeToRepo(repoRoot, p string) (string, error) {
	if !filepath.IsAbs(p) {
		// Treat as already-repo-relative; reject escape attempts.
		clean := filepath.Clean(p)
		if strings.HasPrefix(clean, "..") {
			return "", camperrors.NewValidation("include", "path escapes repo root: "+p, nil)
		}
		return filepath.ToSlash(clean), nil
	}
	rel, err := filepath.Rel(repoRoot, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", camperrors.NewValidation("include", "path outside repo: "+p, nil)
	}
	return filepath.ToSlash(rel), nil
}

func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, p := range in {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// ErrNoWorkitemContext is returned when ComputePlan cannot resolve any
// workitem context. The CLI maps this to exit code 2 with a help hint.
var ErrNoWorkitemContext = camperrors.NewValidation(
	"workitem",
	"no workitem context resolved",
	nil,
)
