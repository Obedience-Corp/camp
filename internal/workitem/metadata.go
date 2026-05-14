package workitem

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
)

const MetadataFilename = ".workitem"

const WorkitemSchemaVersion = "v1alpha4"

const MetadataKind = "workitem"

var acceptedWorkitemVersions = map[string]bool{
	WorkitemSchemaVersion: true,
}

type Metadata struct {
	Version   string `yaml:"version"`
	Kind      string `yaml:"kind"`
	ID        string `yaml:"id"`
	Type      string `yaml:"type"`
	Title     string `yaml:"title,omitempty"`
	CreatedBy string `yaml:"created_by,omitempty"`
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
	if !acceptedWorkitemVersions[m.Version] {
		return camperrors.NewValidation("version",
			"unsupported .workitem schema version (got "+m.Version+", supported: "+WorkitemSchemaVersion+"); update .workitem `version:` to "+WorkitemSchemaVersion, nil)
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
	return nil
}
