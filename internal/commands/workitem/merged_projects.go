package workitem

import (
	"path/filepath"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// annotateProjectLinks records, on each workitem, the projects it reaches
// through links.yaml. A worktree scope resolves to the project that owns the
// worktree: a worktree is a checkout of a project, not a project of its own, so
// a workitem executed in one is related to that project with the worktree as a
// detail. The resolution is path-derived when the row predates the scope's
// project field, which is what keeps the relationship after the worktree
// directory is removed.
//
// A nil registry annotates nothing and is not an error: the caller reports the
// registry problem and the listing stays correct without the annotation.
func annotateProjectLinks(layout links.ScopeLayout, registry *links.Links, items []wkitem.WorkItem) {
	if registry == nil {
		return
	}
	for i := range items {
		items[i].ProjectLinks = projectLinksFor(layout, registry, &items[i])
	}
}

// projectLinksFor returns the distinct projects one workitem reaches through
// the registry, in registry order. When several rows name the same project, a
// primary row's worktree detail wins over a non-primary one's, so the detail
// shown is the one commit routing would use.
func projectLinksFor(layout links.ScopeLayout, registry *links.Links, wi *wkitem.WorkItem) []wkitem.ProjectLink {
	index := map[string]int{}
	var out []wkitem.ProjectLink
	for _, link := range registry.Links {
		if !wkitem.LinkMatchesWorkitem(wi, link.WorkitemID, link.WorkitemKey) {
			continue
		}
		project := link.Scope.ProjectFor(layout)
		if project == "" {
			continue
		}
		candidate := wkitem.ProjectLink{
			Path:     project,
			Worktree: worktreeScopePath(link.Scope),
			Primary:  link.Role == links.RolePrimary,
		}
		at, seen := index[project]
		if !seen {
			index[project] = len(out)
			out = append(out, candidate)
			continue
		}
		if candidate.Primary && !out[at].Primary {
			out[at] = candidate
		}
	}
	return out
}

func worktreeScopePath(scope links.LinkScope) string {
	if scope.Kind != links.ScopeWorktree {
		return ""
	}
	return scope.Path
}

// mergeProjectRefs builds one workitem's merged projects view: every path in
// its semantic projects: list, annotated with whether that project is also the
// workitem-scope primary link in links.yaml, followed by any project the
// workitem reaches only through a link.
//
// The link-derived tail is what stops a worktree-scoped workitem from showing
// no project at all. Each such entry carries the worktree it came through and
// whether that worktree is still on this machine, so the project reads as the
// relationship and the worktree as its detail.
//
// It always returns a non-nil slice so the JSON projects field encodes as []
// rather than null when the list is empty.
func mergeProjectRefs(campaignRoot string, item wkitem.WorkItem, registry *links.Links) []wkitem.ProjectRef {
	refs := make([]wkitem.ProjectRef, 0, len(item.Projects)+len(item.ProjectLinks))
	seen := make(map[string]int, len(item.Projects)+len(item.ProjectLinks))
	for _, path := range item.Projects {
		primary := false
		if registry != nil {
			_, primary = registry.PrimaryForScope(links.ScopeProject, path)
		}
		seen[path] = len(refs)
		refs = append(refs, wkitem.ProjectRef{Path: path, Primary: primary})
	}
	for _, link := range item.ProjectLinks {
		ref := wkitem.ProjectRef{
			Path:            link.Path,
			Worktree:        link.Worktree,
			WorktreeMissing: link.Worktree != "" && !pathExists(filepath.Join(campaignRoot, filepath.FromSlash(link.Worktree))),
		}
		at, listed := seen[link.Path]
		if !listed {
			if registry != nil {
				_, ref.Primary = registry.PrimaryForScope(links.ScopeProject, link.Path)
			}
			seen[link.Path] = len(refs)
			refs = append(refs, ref)
			continue
		}
		// The project is already listed from projects:; attach the worktree
		// detail to that entry rather than repeating the project.
		if refs[at].Worktree == "" {
			refs[at].Worktree = ref.Worktree
			refs[at].WorktreeMissing = ref.WorktreeMissing
		}
	}
	return refs
}
