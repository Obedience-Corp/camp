package workitem

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// AdoptRequest describes a directory to give a .workitem marker.
//
// The caller supplies the identity. Generating an id and a ref needs the set
// of ids and refs already in use, and callers differ in how they have that at
// hand: a command discovers it, while triage's preflight already holds a
// discovery pass it should not repeat.
type AdoptRequest struct {
	// RelPath is the directory, relative to the campaign root.
	RelPath  string
	Type     string
	Title    string
	ID       string
	Ref      string
	QuestID  string
	Tags     []string
	Projects []string
}

// AdoptDirectory writes the .workitem marker that gives a discovered-by-
// location directory a durable identity, and returns what it wrote.
//
// This is the one place the marker is created. `camp workitem adopt` and the
// triage identity preflight both call it, so an item adopted by either route
// is indistinguishable from the other — which is the point: triage must not
// invent a second kind of adopted workitem.
func AdoptDirectory(ctx context.Context, campaignRoot string, req AdoptRequest) (Metadata, error) {
	if err := ctx.Err(); err != nil {
		return Metadata{}, err
	}
	if err := validateAdoptTarget(req.RelPath); err != nil {
		return Metadata{}, err
	}
	if req.ID == "" {
		return Metadata{}, camperrors.NewValidation("id", "is required to adopt "+req.RelPath, nil)
	}
	if req.Ref == "" {
		return Metadata{}, camperrors.NewValidation("ref", "is required to adopt "+req.RelPath, nil)
	}

	target := filepath.Join(campaignRoot, filepath.FromSlash(req.RelPath))
	info, err := os.Stat(target)
	if err != nil {
		return Metadata{}, camperrors.Wrap(err, "stat target dir")
	}
	if !info.IsDir() {
		return Metadata{}, camperrors.NewValidation("dir", "target must be a directory: "+target, nil)
	}

	markerPath := filepath.Join(target, MetadataFilename)
	if _, err := os.Stat(markerPath); err == nil {
		return Metadata{}, camperrors.NewValidation("path",
			"."+strings.TrimPrefix(MetadataFilename, ".")+" already exists at "+markerPath+
				" — directory is already adopted", nil)
	}

	meta := Metadata{
		Version:  WorkitemSchemaVersion,
		Kind:     MetadataKind,
		ID:       req.ID,
		Type:     req.Type,
		Title:    req.Title,
		Ref:      req.Ref,
		QuestID:  req.QuestID,
		Tags:     req.Tags,
		Projects: req.Projects,
	}
	buf, err := yaml.Marshal(&meta)
	if err != nil {
		return Metadata{}, camperrors.Wrap(err, "marshal metadata")
	}
	if err := fsutil.WriteFileAtomically(markerPath, buf, 0o644); err != nil {
		return Metadata{}, err
	}
	return meta, nil
}

// validateAdoptTarget keeps an adoption inside the campaign.
func validateAdoptTarget(relPath string) error {
	if relPath == "" {
		return camperrors.NewValidation("dir", "is required", nil)
	}
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) {
		return camperrors.NewValidation("dir", "parent dir must be relative to camp root", nil)
	}
	if strings.HasPrefix(clean, "..") {
		return camperrors.NewValidation("dir", "parent dir must not escape camp root", nil)
	}
	return nil
}

// IDsFromWorkitems returns the set of workitem ids already in use across the
// provided list. Use it to check a candidate id for collisions without a
// second filesystem walk. Companion to RefsFromWorkitems.
func IDsFromWorkitems(items []WorkItem) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		if item.StableID != "" {
			out[item.StableID] = true
		}
	}
	return out
}
