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
	return item, nil
}
