package artifacts

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/machines"
)

// Committed manifests are the record half of design doc 08: camp never
// versions artifact content, it commits what the artifact set was. One file
// per (machine, root) under a committed directory, so two machines
// legitimately holding different content never conflict: distinct paths by
// construction.

// CommittedRelDir is the committed manifest tree, campaign-relative.
const CommittedRelDir = ".campaign/artifacts/manifests"

// EnvMachineName overrides the machine identity component. It exists for
// containerized tests that must simulate two machines against one filesystem,
// and for hosts whose hostname is not a stable identity.
const EnvMachineName = "CAMP_MACHINE_NAME"

// MachineName is this machine's identity component in committed manifest
// paths. It reuses the machines file's hostname normalization rather than
// inventing a second identity: the same string a peer sees in the mesh is the
// directory their manifests appear under.
func MachineName() (string, error) {
	if v := os.Getenv(EnvMachineName); v != "" {
		return machines.NormalizeHost(v), nil
	}
	host, err := os.Hostname()
	if err != nil {
		return "", camperrors.Wrap(err, "resolve hostname for manifest identity")
	}
	return machines.NormalizeHost(host), nil
}

// CommittedManifestRelPath is the campaign-relative path of one machine's
// committed manifest for one root. The root is slugged with the same
// injective encoding peersync snapshots use, so distinct roots stay distinct.
func CommittedManifestRelPath(machine, rootRel string) string {
	return filepath.ToSlash(filepath.Join(CommittedRelDir, machine, snapshotSlug(rootRel)+".json"))
}

// LoadCommitted reads one machine's committed manifest for a root. A missing
// file is (nil, "", nil): no record yet, which callers treat as a first pass.
func LoadCommitted(campaignRoot, machine, rootRel string) (*Manifest, string, error) {
	path := filepath.Join(campaignRoot, filepath.FromSlash(CommittedManifestRelPath(machine, rootRel)))
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", camperrors.Wrapf(err, "read committed manifest for %s", rootRel)
	}
	var c committedManifest
	if err := json.Unmarshal(data, &c); err != nil {
		// An unreadable record degrades to a first pass rather than wedging
		// the manifest job forever: the next write replaces it.
		return nil, "", nil
	}
	return &Manifest{Version: c.Version, Root: c.Root, Files: c.Files}, c.DescribesCommit, nil
}

// WriteCommitted durably writes one machine's committed manifest for a root
// and returns its campaign-relative path. Temp file plus rename, the same
// crash discipline the queue uses.
func WriteCommitted(campaignRoot, machine string, m *Manifest, describesCommit string) (string, error) {
	rel := CommittedManifestRelPath(machine, m.Root)
	abs := filepath.Join(campaignRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", camperrors.Wrapf(err, "create committed manifest dir for %s", m.Root)
	}
	data, err := m.CommittedJSON(describesCommit)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".manifest-*")
	if err != nil {
		return "", camperrors.Wrapf(err, "stage committed manifest for %s", m.Root)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", camperrors.Wrapf(err, "write committed manifest for %s", m.Root)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", camperrors.Wrapf(err, "close committed manifest for %s", m.Root)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		_ = os.Remove(tmpName)
		return "", camperrors.Wrapf(err, "place committed manifest for %s", m.Root)
	}
	return rel, nil
}

// SameFiles reports whether two file lists record the same artifact state:
// same paths, sizes, mtimes, kinds, and hashes, in the same (sorted) order.
// It is the skip-commit decision: equal files mean the committed record is
// already correct and nothing is rewritten.
func SameFiles(a, b []FileEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Drift is one working-tree divergence from the committed record.
type Drift struct {
	Path   string // slash-separated, relative to the root
	Reason string // "changed", "missing", or "extra"
}

// DetectDrift compares a root's working tree against its committed manifest,
// stat-level only: size or nanosecond mtime moved means changed, an entry
// with no file means missing, a file with no entry means extra. It reports
// and never resolves; hashing is deliberately absent so the comparison is
// cheap enough for a status-path notice detector.
func DetectDrift(ctx context.Context, campaignRoot string, committed *Manifest) ([]Drift, error) {
	current, err := BuildManifest(ctx, campaignRoot, committed.Root)
	if err != nil {
		return nil, err
	}
	base := committed.Index()
	seen := make(map[string]bool, len(current.Files))
	var drifts []Drift
	for _, f := range current.Files {
		seen[f.Path] = true
		b, ok := base[f.Path]
		if !ok {
			drifts = append(drifts, Drift{Path: f.Path, Reason: "extra"})
			continue
		}
		if b.Size != f.Size || b.MTime != f.MTime || b.Symlink != f.Symlink {
			drifts = append(drifts, Drift{Path: f.Path, Reason: "changed"})
		}
	}
	for _, b := range committed.Files {
		if !seen[b.Path] {
			drifts = append(drifts, Drift{Path: b.Path, Reason: "missing"})
		}
	}
	return drifts, nil
}
