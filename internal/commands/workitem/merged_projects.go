package workitem

import (
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// mergeProjectRefs builds one workitem's merged projects view: every path in its
// semantic projects: list, annotated with whether that project is also the
// workitem-scope primary link in links.yaml (registry.PrimaryForScope). The base
// is the projects: list exactly (a primary link to a project not listed there is
// a separate consistency concern, not surfaced here). It always returns a
// non-nil slice so the JSON projects field encodes as [] rather than null when
// the list is empty.
func mergeProjectRefs(projects []string, registry *links.Links) []wkitem.ProjectRef {
	refs := make([]wkitem.ProjectRef, 0, len(projects))
	for _, path := range projects {
		primary := false
		if registry != nil {
			_, primary = registry.PrimaryForScope(links.ScopeProject, path)
		}
		refs = append(refs, wkitem.ProjectRef{Path: path, Primary: primary})
	}
	return refs
}
