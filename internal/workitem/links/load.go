package links

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// linksDir returns `<root>/.campaign/workitems/`.
func linksDir(root string) string {
	return filepath.Join(root, ".campaign", "workitems")
}

// LinksPath returns the absolute path to `links.yaml` for a campaign root.
func LinksPath(root string) string {
	return filepath.Join(linksDir(root), "links.yaml")
}

// CurrentPath returns the absolute path to `current.yaml` for a campaign root.
func CurrentPath(root string) string {
	return filepath.Join(linksDir(root), "current.yaml")
}

// Load reads `links.yaml` and returns the registry. A missing file is the
// documented zero state — Load returns Empty(), no error. Malformed YAML or
// an unknown schema version returns a wrapped error.
func Load(ctx context.Context, root string) (*Links, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(LinksPath(root))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Empty(), nil
		}
		return nil, camperrors.Wrap(err, "read links.yaml")
	}

	var out Links
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, camperrors.Wrap(err, "parse links.yaml")
	}
	if out.Version == "" {
		return nil, newValidation("version", "links.yaml is missing the version field")
	}
	if out.Version != LinksSchemaVersion {
		return nil, newValidation("version",
			"unknown links.yaml schema version "+out.Version+
				"; this build understands "+LinksSchemaVersion)
	}
	if out.Links == nil {
		out.Links = []Link{}
	}
	return &out, nil
}

// LoadCurrent reads `current.yaml` and returns the selection. A missing file
// returns nil, nil.
func LoadCurrent(ctx context.Context, root string) (*Current, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(CurrentPath(root))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, camperrors.Wrap(err, "read current.yaml")
	}

	var out Current
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, camperrors.Wrap(err, "parse current.yaml")
	}
	if out.Version == "" {
		return nil, newValidation("version", "current.yaml is missing the version field")
	}
	if out.Version != CurrentSchemaVersion {
		return nil, newValidation("version",
			"unknown current.yaml schema version "+out.Version+
				"; this build understands "+CurrentSchemaVersion)
	}
	return &out, nil
}
