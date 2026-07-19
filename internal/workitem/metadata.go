package workitem

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
)

var (
	refShape     = regexp.MustCompile(`^WI-[0-9a-f]{6}$`)
	questIDShape = regexp.MustCompile(`^qst_[A-Za-z0-9_-]{1,40}$`)
	tagShape     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	versionShape = regexp.MustCompile(`^v1alpha([0-9]+)$`)
)

const MetadataFilename = ".workitem"

const WorkitemSchemaVersion = "v1alpha8"

const MetadataKind = "workitem"

// acceptedWorkitemVersions are loadable .workitem schema versions. v1alpha4
// through v1alpha7 are accepted for backward compatibility (v1alpha7 added the
// GatheredInto/GatheredAt fields); v1alpha8 is the current write version and
// adds the Tags and Projects fields. validateMetadata additionally accepts
// unknown future v1alphaN via the forward-compat rule.
var acceptedWorkitemVersions = map[string]bool{
	"v1alpha4": true,
	"v1alpha5": true,
	"v1alpha6": true,
	"v1alpha7": true,
	"v1alpha8": true,
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
	// PromotedTo records the destination a workitem was promoted to
	// (festivals/<dir> or docs/<dest>). Set by `camp workitem promote`.
	PromotedTo string `yaml:"promoted_to,omitempty"`
	// PromotedAt is the RFC3339 UTC timestamp of the promotion.
	PromotedAt string `yaml:"promoted_at,omitempty"`
	// GatheredInto records the id of the combined workitem this workitem was
	// merged into by `camp gather`. Set on source workitems when their
	// directories are moved inside the gathered package. Added in v1alpha7.
	GatheredInto string `yaml:"gathered_into,omitempty"`
	// GatheredAt is the RFC3339 UTC timestamp of the gather. Added in v1alpha7.
	GatheredAt string `yaml:"gathered_at,omitempty"`
	// Tags is a free-form list of topic labels. Normalized to lowercase
	// kebab-case on write; validated against tagShape on load. Added in v1alpha8.
	Tags []string `yaml:"tags,omitempty"`
	// Projects is a set of campaign-relative paths under projects/ this
	// workitem is semantically related to. Added in v1alpha8. The
	// authoritative store for this fact; see internal/workitem/links for the
	// separate operational link registry (primary/worktree/blocked_by/supersedes).
	// See workflow/design/workitem-schema-tags-and-projects/04-projects-and-links.md.
	Projects []string `yaml:"projects,omitempty"`
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
	switch {
	case acceptedWorkitemVersions[m.Version]:
	case IsForwardCompatVersion(m.Version):
		slog.Default().Warn("workitem schema version is newer than this binary; loading via forward-compat and dropping unknown fields",
			"version", m.Version, "current", WorkitemSchemaVersion)
	default:
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
	if err := validateTags(m.Tags); err != nil {
		return err
	}
	if err := validateProjects(m.Projects); err != nil {
		return err
	}
	return nil
}

func validateTags(tags []string) error {
	seen := make(map[string]bool, len(tags))
	for _, tag := range tags {
		if !tagShape.MatchString(tag) {
			return camperrors.NewValidation("tags",
				"tag must match "+tagShape.String()+", got "+tag, nil)
		}
		if seen[tag] {
			return camperrors.NewValidation("tags", "duplicate tag: "+tag, nil)
		}
		seen[tag] = true
	}
	return nil
}

func validateProjects(paths []string) error {
	seen := make(map[string]bool, len(paths))
	for _, p := range paths {
		if p == "" {
			return camperrors.NewValidation("projects", "project path must not be empty", nil)
		}
		if filepath.IsAbs(p) {
			return camperrors.NewValidation("projects",
				"project path must be campaign-relative, got absolute path "+p, nil)
		}
		if strings.HasPrefix(filepath.Clean(p), "..") {
			return camperrors.NewValidation("projects",
				"project path must not escape the campaign root, got "+p, nil)
		}
		if seen[p] {
			return camperrors.NewValidation("projects", "duplicate project path: "+p, nil)
		}
		seen[p] = true
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

// IsAcceptedVersion reports whether v is a loadable .workitem schema version.
func IsAcceptedVersion(v string) bool {
	return acceptedWorkitemVersions[v]
}

// IsCurrentVersion reports whether v is the current .workitem schema version.
func IsCurrentVersion(v string) bool {
	return v == WorkitemSchemaVersion
}

// IsForwardCompatVersion reports whether v is a v1alphaN schema version newer
// than every accepted version. Such a marker loads under the forward-compat
// rule with a warning, and fields the loader does not model are dropped by the
// non-strict YAML parse. It returns false for accepted or legacy versions.
func IsForwardCompatVersion(v string) bool {
	if acceptedWorkitemVersions[v] {
		return false
	}
	match := versionShape.FindStringSubmatch(v)
	if match == nil {
		return false
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		return false
	}
	return n > maxAcceptedVersion()
}

func maxAcceptedVersion() int {
	highest := 0
	for v := range acceptedWorkitemVersions {
		match := versionShape.FindStringSubmatch(v)
		if match == nil {
			continue
		}
		if n, err := strconv.Atoi(match[1]); err == nil && n > highest {
			highest = n
		}
	}
	return highest
}

// ValidRef reports whether s is a well-formed workitem ref (WI-<6 hex>).
func ValidRef(s string) bool {
	return refShape.MatchString(s)
}

// ValidQuestID reports whether s is a well-formed quest id (qst_<id>).
func ValidQuestID(s string) bool {
	return questIDShape.MatchString(s)
}

// ValidTag reports whether s is a well-formed tag (lowercase kebab-case).
func ValidTag(s string) bool {
	return tagShape.MatchString(s)
}

// ValidateProjectPaths reports the first shape problem among project paths
// (empty, absolute, escaping the campaign root, or duplicate), or nil. It is
// the exported form of the loader's projects validation.
func ValidateProjectPaths(paths []string) error {
	return validateProjects(paths)
}
