package workitem

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
)

var (
	refShape     = regexp.MustCompile(`^WI-[0-9a-f]{6}$`)
	questIDShape = regexp.MustCompile(`^qst_[A-Za-z0-9_-]{1,40}$`)
)

const MetadataFilename = ".workitem"

const WorkitemSchemaVersion = "v1alpha6"

const MetadataKind = "workitem"

// acceptedWorkitemVersions are loadable .workitem schema versions. v1alpha4
// and v1alpha5 are accepted for backward compatibility; v1alpha6 is the
// current shape and gains the Ref and QuestID fields.
var acceptedWorkitemVersions = map[string]bool{
	"v1alpha4": true,
	"v1alpha5": true,
	"v1alpha6": true,
}

type Metadata struct {
	Version   string `yaml:"version"`
	Kind      string `yaml:"kind"`
	ID        string `yaml:"id"`
	Type      string `yaml:"type"`
	Title     string `yaml:"title,omitempty"`
	CreatedBy string `yaml:"created_by,omitempty"`
	// Ref is a deterministic short reference for commit-message embedding.
	// Format: WI-<6 lowercase hex>. Added in v1alpha6; absent on legacy
	// workitems and backfilled by `camp workitem doctor --fix`.
	Ref string `yaml:"ref,omitempty"`
	// QuestID is the id of the quest active when this workitem was created
	// or adopted. Empty when no quest resolved (no --quest flag and no
	// CAMP_QUEST env var). Added in v1alpha6.
	QuestID string `yaml:"quest_id,omitempty"`
}

// LoadMetadata reads .workitem from dir on the host filesystem.
// Returns (nil, nil) when the file does not exist.
func LoadMetadata(ctx context.Context, dir string) (*Metadata, error) {
	abs, err := filepath.Abs(filepath.Join(dir, MetadataFilename))
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolving %s", dir)
	}
	return LoadMetadataFS(ctx, os.DirFS("/"), strings.TrimPrefix(abs, "/"))
}

// LoadMetadataFS reads .workitem from path inside fsys.
// Returns (nil, nil) when the file does not exist.
// Used by tests to drive validation paths via fstest.MapFS without
// touching the host filesystem (D029).
func LoadMetadataFS(ctx context.Context, fsys fs.FS, path string) (*Metadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	raw, err := fs.ReadFile(fsys, path)
	if errors.Is(err, fs.ErrNotExist) {
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
			"unsupported .workitem schema version (got "+m.Version+", supported: "+
				strings.Join(supportedWorkitemVersions(), ", ")+"); update .workitem `version:` to "+WorkitemSchemaVersion, nil)
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
	if m.Ref != "" && !refShape.MatchString(m.Ref) {
		return camperrors.NewValidation("ref",
			"ref must match WI-<6 hex>, got "+m.Ref, nil)
	}
	if m.QuestID != "" && !questIDShape.MatchString(m.QuestID) {
		return camperrors.NewValidation("quest_id",
			"quest_id must match qst_<id>, got "+m.QuestID, nil)
	}
	return nil
}

func supportedWorkitemVersions() []string {
	versions := make([]string, 0, len(acceptedWorkitemVersions))
	for version := range acceptedWorkitemVersions {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	return versions
}
