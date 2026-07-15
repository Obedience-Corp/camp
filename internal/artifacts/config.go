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
	if !knownPolicy(root.Policy) {
		return camperrors.Newf("unknown policy %q (want %s or %s)", root.Policy, PolicyAlways, PolicyOnDemand)
	}
	if _, exists := f.Find(root.Path); exists {
		return camperrors.Newf("artifact root %q is already declared", root.Path)
	}
	f.Roots = append(f.Roots, root)
	return nil
}

// knownPolicy reports whether p is a recognized root policy (empty means the
// default, PolicyAlways).
func knownPolicy(p string) bool {
	switch p {
	case "", PolicyAlways, PolicyOnDemand:
		return true
	default:
		return false
	}
}

// Validate checks that the whole declaration is well-formed before the
// sync/pull engine consumes it: a supported version, known policies,
// campaign-relative paths, and no duplicate roots. Load stays lenient so the
// edit commands can still open a broken file to repair it; the data-moving
// paths (pull and verify) call Validate so a hand-edited or maliciously
// committed artifacts.yaml fails closed instead of being partially honored -
// an unknown policy is otherwise silently skipped on a normal sync but pulled
// under --artifacts-only, and a duplicate root writes the same snapshot twice.
func (f *File) Validate() error {
	if f.Version != 1 {
		return camperrors.Newf("unsupported artifacts.yaml version %d (want 1)", f.Version)
	}
	seen := make(map[string]bool, len(f.Roots))
	for _, r := range f.Roots {
		if err := ValidateRootPath(r.Path); err != nil {
			return err
		}
		if !knownPolicy(r.Policy) {
			return camperrors.Newf("unknown policy %q for root %q (want %s or %s)", r.Policy, r.Path, PolicyAlways, PolicyOnDemand)
		}
		norm := NormalizeRootPath(r.Path)
		if seen[norm] {
			return camperrors.Newf("duplicate artifact root %q", norm)
		}
		seen[norm] = true
	}
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
	if strings.EqualFold(normalized, ".campaign") || hasCaseInsensitivePrefix(normalized, ".campaign/") {
		return camperrors.Newf("artifact root %q may not live under .campaign", path)
	}
	return nil
}

// hasCaseInsensitivePrefix reports whether s starts with prefix, ignoring
// case. The .campaign guard uses it so a case-insensitive filesystem (APFS,
// NTFS) cannot smuggle a ".Campaign/" root past the check.
func hasCaseInsensitivePrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// EnsureRootWithin re-validates a root about to be consumed by the sync/pull
// engine, not just declared: it re-runs ValidateRootPath (defending against a
// hand-edited or malicious .campaign/artifacts.yaml that never went through
// Add) and then resolves symlinks so a symlinked root cannot redirect rsync
// writes outside the campaign. Returns the cleaned, campaign-relative root on
// success.
func EnsureRootWithin(campaignRoot, rootPath string) (string, error) {
	if err := ValidateRootPath(rootPath); err != nil {
		return "", err
	}
	normalized := NormalizeRootPath(rootPath)

	campReal, err := filepath.EvalSymlinks(campaignRoot)
	if err != nil {
		campReal = campaignRoot // campaign root should exist; fall back to literal
	}
	abs := filepath.Join(campReal, filepath.FromSlash(normalized))

	// Resolve the longest existing prefix of the target: a not-yet-created
	// root is fine, but any existing symlink component must stay inside the
	// campaign.
	real, err := resolveExistingPrefix(abs)
	if err != nil {
		return "", camperrors.Wrapf(err, "resolve artifact root %s", normalized)
	}
	rel, err := filepath.Rel(campReal, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", camperrors.Newf("artifact root %q resolves outside the campaign root", rootPath)
	}
	return normalized, nil
}

// resolveExistingPrefix EvalSymlinks the longest existing ancestor of p and
// re-appends the non-existent tail, so a root that does not exist yet can
// still be checked for symlinked ancestors that escape the campaign.
func resolveExistingPrefix(p string) (string, error) {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real, nil
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p, nil
	}
	realParent, err := resolveExistingPrefix(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(realParent, filepath.Base(p)), nil
}
