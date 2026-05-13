package workitem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
)

// MetadataFilename is the canonical name of the workitem metadata file.
const MetadataFilename = ".workitem"

// MetadataVersion is the only schema version this parser accepts.
const MetadataVersion = 1

// MetadataKind is the only kind this parser accepts.
const MetadataKind = "workitem"

// Metadata is the typed view of a .workitem file. All nested blocks are
// optional in v1; only top-level Version, Kind, ID, Type, and Title are
// required.
type Metadata struct {
	Version int    `yaml:"version"`
	Kind    string `yaml:"kind"`

	ID          string `yaml:"id"`
	Type        string `yaml:"type"`
	Title       string `yaml:"title"`
	Description string `yaml:"description,omitempty"`

	Collection *MetadataCollection `yaml:"collection,omitempty"`
	Execution  *MetadataExecution  `yaml:"execution,omitempty"`
	Priority   *MetadataPriority   `yaml:"priority,omitempty"`
	Project    *MetadataProject    `yaml:"project,omitempty"`
	Workflow   *MetadataWorkflow   `yaml:"workflow,omitempty"`
	Lineage    *MetadataLineage    `yaml:"lineage,omitempty"`
	External   *MetadataExternal   `yaml:"external,omitempty"`
}

type MetadataCollection struct {
	Root            string `yaml:"root,omitempty"`
	RelativePath    string `yaml:"relative_path,omitempty"`
	LifecycleStatus string `yaml:"lifecycle_status,omitempty"`
}

type MetadataExecution struct {
	Mode          string `yaml:"mode,omitempty"`
	Autonomy      string `yaml:"autonomy,omitempty"`
	Risk          string `yaml:"risk,omitempty"`
	BlockedReason string `yaml:"blocked_reason,omitempty"`
}

type MetadataPriority struct {
	Level  string `yaml:"level,omitempty"`
	Reason string `yaml:"reason,omitempty"`
}

type MetadataProject struct {
	Name string `yaml:"name,omitempty"`
	Path string `yaml:"path,omitempty"`
	Role string `yaml:"role,omitempty"`
}

type MetadataWorkflow struct {
	DocPath     string `yaml:"doc_path,omitempty"`
	RuntimeDir  string `yaml:"runtime_dir,omitempty"`
	WorkflowID  string `yaml:"workflow_id,omitempty"`
	ActiveRunID string `yaml:"active_run_id,omitempty"`
}

type MetadataLineage struct {
	PromotedFrom []string `yaml:"promoted_from,omitempty"`
	PromotedTo   []string `yaml:"promoted_to,omitempty"`
	Supersedes   []string `yaml:"supersedes,omitempty"`
}

type MetadataExternal struct {
	SpecKit *MetadataSpecKit `yaml:"spec_kit,omitempty"`
}

type MetadataSpecKit struct {
	Enabled    bool   `yaml:"enabled,omitempty"`
	SpecifyDir string `yaml:"specify_dir,omitempty"`
}

// LoadMetadata reads .workitem from dir and returns the parsed metadata.
// Returns (nil, nil) when the file does not exist (legacy path).
// Returns a contextual error for any other read or validation failure.
func LoadMetadata(ctx context.Context, dir string) (*Metadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path := filepath.Join(dir, MetadataFilename)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "reading %s", path)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var m Metadata
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, camperrors.Wrapf(err, "parsing %s", path)
	}

	if err := validateMetadata(&m); err != nil {
		return nil, camperrors.Wrapf(err, "validating %s", path)
	}

	return &m, nil
}

func validateMetadata(m *Metadata) error {
	if m.Version != MetadataVersion {
		return camperrors.NewValidation("version",
			"unsupported workitem schema version (want "+strconv.Itoa(MetadataVersion)+")", nil)
	}
	if m.Kind != MetadataKind {
		return camperrors.NewValidation("kind",
			"unsupported kind (want "+MetadataKind+")", nil)
	}
	if m.ID == "" {
		return camperrors.NewValidation("id", "required field id is empty", nil)
	}
	if m.Type == "" {
		return camperrors.NewValidation("type", "required field type is empty", nil)
	}
	if m.Title == "" {
		return camperrors.NewValidation("title", "required field title is empty", nil)
	}
	return nil
}
