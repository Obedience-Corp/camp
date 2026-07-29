package workitem

import (
	"context"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// migrateRelatedProjectLink appends link.Scope.Path to the projects: list of the
// workitem identified by link.WorkitemID, writing to its .workitem marker
// (directory) or its frontmatter (file). It is idempotent: when the path is
// already listed it returns nil without writing, so the caller (doctor --fix)
// only removes the row. It never removes the link row itself — the caller does
// that, and only after this succeeds, so an unresolvable workitem preserves its
// data.
func migrateRelatedProjectLink(ctx context.Context, root string, link links.Link) error {
	wi, err := resolveSelector(ctx, root, link.WorkitemID, false)
	if err != nil {
		return camperrors.Wrapf(err, "resolving workitem %s for project migration", link.WorkitemID)
	}

	if wi.ItemKind == wkitem.ItemKindDirectory {
		absMarker := filepath.Join(root, filepath.FromSlash(wi.RelativePath), wkitem.MetadataFilename)
		raw, err := os.ReadFile(absMarker)
		if err != nil {
			return camperrors.Wrapf(err, "reading %s", absMarker)
		}
		var meta wkitem.Metadata
		if err := yaml.Unmarshal(raw, &meta); err != nil {
			return camperrors.Wrapf(err, "parsing %s", absMarker)
		}
		for _, p := range meta.Projects {
			if p == link.Scope.Path {
				return nil // already migrated, idempotent no-op
			}
		}
		meta.Projects = append(meta.Projects, link.Scope.Path)
		// A directory .workitem holds only camp-owned keys, so a full-struct
		// remarshal is safe (no foreign keys to preserve, unlike frontmatter).
		out, err := yaml.Marshal(&meta)
		if err != nil {
			return camperrors.Wrap(err, "marshal updated .workitem")
		}
		return fsutil.WriteFileAtomically(absMarker, out, 0o644)
	}

	// File (frontmatter) workitem: stamp through the AST-preserving helper so the
	// body and unrelated frontmatter keys are never touched.
	absFile := filepath.Join(root, filepath.FromSlash(wi.RelativePath))
	existing, err := wkitem.LoadFrontmatterMetadata(absFile)
	if err != nil {
		return camperrors.Wrapf(err, "reading frontmatter %s", absFile)
	}
	if existing == nil {
		return camperrors.NewValidation("frontmatter",
			"no kind: workitem frontmatter to migrate into at "+absFile, nil)
	}
	for _, p := range existing.Projects {
		if p == link.Scope.Path {
			return nil
		}
	}
	newProjects := append(append([]string{}, existing.Projects...), link.Scope.Path)
	return wkitem.StampFrontmatterFields(ctx, absFile,
		[]wkitem.FrontmatterField{{After: frontmatterProjectsAnchor(existing), Key: "projects", Values: newProjects}})
}

// frontmatterProjectsAnchor returns the key a freshly stamped projects: list
// should follow so a doctor-migrated file lands projects: exactly where
// create/adopt would (campStampFields, adopt_file.go): chained after the last
// camp key present before it. type is always present, so the anchor never falls
// through to insertNodeAfter's append-at-end path.
func frontmatterProjectsAnchor(meta *wkitem.Metadata) string {
	switch {
	case len(meta.Tags) > 0:
		return "tags"
	case meta.QuestID != "":
		return "quest_id"
	case meta.Ref != "":
		return "ref"
	case meta.Title != "":
		return "title"
	default:
		return "type"
	}
}
