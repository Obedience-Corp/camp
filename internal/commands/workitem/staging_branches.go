package workitem

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
)

var (
	osGetwd = os.Getwd
	osStat  = os.Stat
)

// cwdSubGitRepo returns the absolute path of a sub-git-repo containing cwd
// when cwd is strictly inside the campaign tree but not the campaign root
// itself. Empty cwd resolves via os.Getwd.
func cwdSubGitRepo(cwd, campaignRoot string) (string, bool) {
	if cwd == "" {
		c, err := osGetwd()
		if err != nil {
			return "", false
		}
		cwd = c
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(campaignRoot, abs)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return "", false
	}
	dir := abs
	for dir != campaignRoot && len(dir) > len(campaignRoot) {
		gitMarker := filepath.Join(dir, ".git")
		if info, err := osStat(gitMarker); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return dir, true
		}
		dir = filepath.Dir(dir)
	}
	return "", false
}

func computeDetectedProjectPlan(ctx context.Context, opts PlanOptions, plan *StagingPlan, repoRoot string) (*StagingPlan, error) {
	plan.RepoRoot = repoRoot
	plan.Context = PlanContextLinkedProject
	plan.ContextNote = "project " + filepath.Base(repoRoot)
	return finishProjectPlan(ctx, opts, plan)
}

func finishProjectPlan(ctx context.Context, opts PlanOptions, plan *StagingPlan) (*StagingPlan, error) {
	stage, err := listChangedFilesUnder(ctx, plan.RepoRoot, "")
	if err != nil {
		return nil, camperrors.Wrap(err, "list project changes")
	}
	added, addSkip, err := applyIncludes(plan.RepoRoot, stage, opts.Includes)
	if err != nil {
		return nil, err
	}
	plan.Skip = append(plan.Skip, addSkip...)
	plan.Stage = applyExcludes(added, opts.Excludes, &plan.Skip)
	plan.Stage = dedupeSorted(plan.Stage)
	return plan, nil
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
		plan.addStageNote(linkRegistryRelPath, stageAnnotationLinkRegistry)
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
		plan.addStageNote(linkRegistryRelPath, stageAnnotationLinkRegistry)
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
