package workitem

import (
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func ApplyMetadata(item WorkItem, md *Metadata) (WorkItem, error) {
	if md == nil {
		return item, nil
	}
	if md.ID == "" {
		return item, camperrors.NewValidation("id",
			"workitem metadata missing id (path "+item.RelativePath+"): add `id:` to .workitem", nil)
	}
	if md.Type == "" {
		return item, camperrors.NewValidation("type",
			"workitem metadata missing type (path "+item.RelativePath+"): add `type:` to .workitem (e.g., type: feature)", nil)
	}
	item.StableID = md.ID
	if md.Title != "" {
		item.Title = md.Title
	}
	if md.Ref != "" || md.QuestID != "" {
		if item.SourceMetadata == nil {
			item.SourceMetadata = make(map[string]any)
		}
		if md.Ref != "" {
			item.SourceMetadata["ref"] = md.Ref
		}
		if md.QuestID != "" {
			item.SourceMetadata["quest_id"] = md.QuestID
		}
	}
	item.Tags = nonNilStrings(md.Tags)
	item.Projects = nonNilStrings(md.Projects)
	// Build the non-nil base merged view here so ProjectRefs is [] not null even
	// on a WorkItem that never reaches the output layer. outputJSON re-derives it
	// with the primary annotation from links.yaml (which is unavailable here).
	item.ProjectRefs = projectRefsBase(item.Projects)
	return item, nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// projectRefsBase renders projects: as the merged view without the links-derived
// primary annotation (every entry primary=false). It always returns a non-nil
// slice so the JSON projects field encodes as [] rather than null.
func projectRefsBase(projects []string) []ProjectRef {
	refs := make([]ProjectRef, 0, len(projects))
	for _, p := range projects {
		refs = append(refs, ProjectRef{Path: p})
	}
	return refs
}
