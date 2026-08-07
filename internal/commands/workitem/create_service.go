package workitem

import (
	"context"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// CreateWorkitemRequest is one directory workitem to create.
type CreateWorkitemRequest struct {
	Slug string
	Type string
	// Title defaults to the slug when empty, matching the command.
	Title string
	// IDOverride pins the generated id; empty derives one.
	IDOverride string
	// DirOverride places the workitem somewhere other than workflow/<type>.
	DirOverride string
	QuestID     string
	Tags        []string
	Projects    []string
}

// CreatedWorkitem is what a creation produced.
type CreatedWorkitem struct {
	ID           string
	Ref          string
	Type         string
	Title        string
	QuestID      string
	RelativePath string
	AbsPath      string
}

// CreateWorkitemDir creates a directory workitem and its `.workitem` marker.
//
// This is the core `camp workitem create` runs, lifted out of the command so
// other verbs can create a workitem the same way rather than reimplementing id
// generation, ref derivation, and marker writing. `camp workitem split` uses
// it for each `--into` successor.
//
// It creates the directory and the marker and nothing else — no README, no
// scaffold — exactly as the command does. A caller that wants seeded content
// writes it afterwards.
func CreateWorkitemDir(
	ctx context.Context, campaignRoot string, cfg *config.CampaignConfig,
	req CreateWorkitemRequest,
) (*CreatedWorkitem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateSlug(req.Slug); err != nil {
		return nil, err
	}
	if err := validateSlug(req.Type); err != nil {
		return nil, camperrors.NewValidation("type", "invalid type slug: "+err.Error(), nil)
	}

	normalizedTags, err := normalizeTags(req.Tags)
	if err != nil {
		return nil, err
	}
	normalizedProjects, err := normalizeProjects(req.Projects)
	if err != nil {
		return nil, err
	}
	if err := wkitem.ValidateProjectPaths(normalizedProjects); err != nil {
		return nil, err
	}

	id, err := generateID(ctx, req.Type, req.Slug, req.IDOverride, campaignRoot)
	if err != nil {
		return nil, err
	}

	parent := req.DirOverride
	if parent == "" {
		parent = filepath.Join("workflow", req.Type)
	}
	if err := validateParentPath(parent); err != nil {
		return nil, err
	}

	ref, err := deriveUniqueRef(ctx, campaignRoot, cfg, id)
	if err != nil {
		return nil, err
	}

	target := filepath.Join(campaignRoot, parent, req.Slug)
	// An existing directory still requires explicit adopt. Silently attaching
	// a marker to whatever is already there is how a split would swallow an
	// unrelated directory.
	if _, err := os.Stat(target); err == nil {
		return nil, camperrors.NewValidation("path",
			"target directory already exists: "+target+
				" — use `camp workitem adopt` to attach metadata to an existing dir", nil)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, camperrors.Wrap(err, "create directory")
	}

	markerWritten := false
	defer func() {
		if !markerWritten {
			// Clean up only the directory this call created, so a failure
			// part-way does not leave a half-made workitem behind.
			_ = os.Remove(target)
		}
	}()

	title := req.Title
	if title == "" {
		title = req.Slug
	}
	meta := wkitem.Metadata{
		Version:  wkitem.WorkitemSchemaVersion,
		Kind:     "workitem",
		ID:       id,
		Type:     req.Type,
		Title:    title,
		Ref:      ref,
		QuestID:  req.QuestID,
		Tags:     normalizedTags,
		Projects: normalizedProjects,
	}
	buf, err := yaml.Marshal(&meta)
	if err != nil {
		return nil, camperrors.Wrap(err, "marshal metadata")
	}
	if err := fsutil.WriteFileAtomically(
		filepath.Join(target, wkitem.MetadataFilename), buf, 0o644); err != nil {
		return nil, err
	}
	markerWritten = true

	return &CreatedWorkitem{
		ID:           id,
		Ref:          ref,
		Type:         req.Type,
		Title:        title,
		QuestID:      req.QuestID,
		RelativePath: filepath.ToSlash(filepath.Join(parent, req.Slug)),
		AbsPath:      target,
	}, nil
}
