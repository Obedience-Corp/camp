// Package artifacts gives campaign media and other heavy non-git payloads a
// declared, syncable home. A committed declaration file (.campaign/
// artifacts.yaml) names artifact roots; a per-machine manifest describes what
// a root currently holds; and an rsync-based pull moves a peer's copy over
// the same machine trust the rest of camp's multi-machine stack uses. Git
// content never travels through this package: declared roots are expected to
// be gitignored, and everything here is delta transport for the filesystem
// class of a campaign.
package artifacts

import (
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
)

// ConfigRelPath is the campaign-relative path of the committed declaration
// file. Committed on purpose: a fresh machine learns what it is missing from
// the declaration, while manifests and snapshots stay machine-local derived
// state under .campaign/cache.
const ConfigRelPath = ".campaign/artifacts.yaml"

// Root policies.
const (
	// PolicyAlways syncs the root on every peer-assisted sync.
	PolicyAlways = "always"
	// PolicyOnDemand syncs the root only when artifacts are requested
	// explicitly (--artifacts-only).
	PolicyOnDemand = "on-demand"
)

// File is the on-disk shape of .campaign/artifacts.yaml.
type File struct {
	Version int    `yaml:"version"`
	Roots   []Root `yaml:"roots"`
}

// Root declares one artifact root.
type Root struct {
	// Path is the campaign-relative directory holding the artifacts.
	Path string `yaml:"path"`
	// Policy is PolicyAlways (default when empty) or PolicyOnDemand.
	Policy string `yaml:"policy,omitempty"`
}

// EffectivePolicy resolves an empty policy to PolicyAlways.
func (r Root) EffectivePolicy() string {
	if r.Policy == "" {
		return PolicyAlways
	}
	return r.Policy
}

// ConfigPath returns the absolute path of the declaration file.
func ConfigPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, filepath.FromSlash(ConfigRelPath))
}

// Load reads the campaign's artifact declarations. A missing file is an
// empty declaration, not an error.
func Load(campaignRoot string) (*File, error) {
	data, err := os.ReadFile(ConfigPath(campaignRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return &File{Version: 1}, nil
		}
		return nil, camperrors.Wrap(err, "read artifacts config")
	}
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, camperrors.Wrap(err, "parse artifacts config")
	}
	if f.Version == 0 {
		f.Version = 1
	}
	return &f, nil
}

// Save writes the declaration file, creating .campaign if needed.
func (f *File) Save(campaignRoot string) error {
	path := ConfigPath(campaignRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return camperrors.Wrap(err, "create .campaign directory")
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return camperrors.Wrap(err, "encode artifacts config")
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return camperrors.Wrap(err, "write artifacts config")
	}
	return nil
}

// Find returns the declared root matching the (normalized) path.
func (f *File) Find(path string) (Root, bool) {
	normalized := NormalizeRootPath(path)
	for _, r := range f.Roots {
		if NormalizeRootPath(r.Path) == normalized {
			return r, true
		}
	}
	return Root{}, false
}

// Add appends a validated root declaration. The path must be relative,
// inside the campaign, and not already declared.
func (f *File) Add(root Root) error {
	if err := ValidateRootPath(root.Path); err != nil {
		return err
	}
	root.Path = NormalizeRootPath(root.Path)
	switch root.Policy {
	case "", PolicyAlways, PolicyOnDemand:
	default:
		return camperrors.Newf("unknown policy %q (want %s or %s)", root.Policy, PolicyAlways, PolicyOnDemand)
	}
	if _, exists := f.Find(root.Path); exists {
		return camperrors.Newf("artifact root %q is already declared", root.Path)
	}
	f.Roots = append(f.Roots, root)
	return nil
}

// Remove drops the declared root matching path, reporting whether it existed.
func (f *File) Remove(path string) bool {
	normalized := NormalizeRootPath(path)
	for i, r := range f.Roots {
		if NormalizeRootPath(r.Path) == normalized {
			f.Roots = append(f.Roots[:i], f.Roots[i+1:]...)
			return true
		}
	}
	return false
}

// NormalizeRootPath cleans a declared path to slash-separated form without
// leading ./ or trailing /.
func NormalizeRootPath(path string) string {
	return strings.Trim(filepath.ToSlash(filepath.Clean(path)), "/")
}

// ValidateRootPath rejects paths that are absolute, empty, escape the
// campaign root, or live under .campaign. Absoluteness is checked on the raw
// input (normalization strips leading slashes); the remaining checks run on
// the normalized form so ./-prefixed spellings cannot dodge them.
func ValidateRootPath(path string) error {
	if filepath.IsAbs(path) {
		return camperrors.Newf("artifact root %q must be relative to the campaign root", path)
	}
	normalized := NormalizeRootPath(path)
	if normalized == "" || normalized == "." {
		return camperrors.New("artifact root path must not be empty")
	}
	if !filepath.IsLocal(filepath.FromSlash(normalized)) {
		return camperrors.Newf("artifact root %q escapes the campaign root", path)
	}
	if normalized == ".campaign" || strings.HasPrefix(normalized, ".campaign/") {
		return camperrors.Newf("artifact root %q may not live under .campaign", path)
	}
	return nil
}
