package artifacts

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Manifest describes one artifact root's current contents on one machine:
// derived state, rebuilt from the filesystem whenever needed, never
// committed. Size and mtime identify a file version; content hashing is a
// deliberate non-default for multi-hundred-GB media roots.
type Manifest struct {
	Version     int         `json:"version"`
	Root        string      `json:"root"`
	GeneratedAt time.Time   `json:"generated_at"`
	Files       []FileEntry `json:"files"`
}

// FileEntry is one regular file inside an artifact root.
type FileEntry struct {
	Path  string `json:"path"` // slash-separated, relative to the root
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime_unix"`
}

// BuildManifest walks the artifact root and records every regular file.
// Symlinks and directories are skipped: an artifact root is expected to hold
// plain payload files. A root directory that does not exist yet yields an
// empty manifest.
func BuildManifest(ctx context.Context, campaignRoot, rootRel string) (*Manifest, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	m := &Manifest{Version: 1, Root: NormalizeRootPath(rootRel), GeneratedAt: time.Now().UTC()}
	rootAbs := filepath.Join(campaignRoot, filepath.FromSlash(m.Root))

	info, err := os.Stat(rootAbs)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "stat artifact root %s", m.Root)
	}
	if !info.IsDir() {
		return nil, camperrors.Newf("artifact root %s is not a directory", m.Root)
	}

	walkErr := filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return err
		}
		m.Files = append(m.Files, FileEntry{
			Path:  filepath.ToSlash(rel),
			Size:  fi.Size(),
			MTime: fi.ModTime().Unix(),
		})
		return nil
	})
	if walkErr != nil {
		return nil, camperrors.Wrapf(walkErr, "walk artifact root %s", m.Root)
	}

	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	return m, nil
}

// Index returns the manifest's files keyed by relative path.
func (m *Manifest) Index() map[string]FileEntry {
	idx := make(map[string]FileEntry, len(m.Files))
	for _, f := range m.Files {
		idx[f.Path] = f
	}
	return idx
}

// ProtectedPaths lists the local files a pull must not overwrite: everything
// whose current state is not exactly the last agreed baseline with that peer.
// That covers files modified since the last transfer AND files the baseline
// has never seen (unknown provenance). With no baseline at all (first sync
// with this peer), every existing local file is protected. The rule errs
// toward never losing local bytes; a pull can only update files whose local
// state is known-agreed.
func (m *Manifest) ProtectedPaths(baseline *Manifest) []string {
	var base map[string]FileEntry
	if baseline != nil {
		base = baseline.Index()
	}
	var protected []string
	for _, f := range m.Files {
		b, ok := base[f.Path]
		if !ok || b.Size != f.Size || b.MTime != f.MTime {
			protected = append(protected, f.Path)
		}
	}
	return protected
}

// VerifyResult compares a local tree against a reference manifest.
type VerifyResult struct {
	Root    string   `json:"root"`
	Checked int      `json:"checked"`
	Missing []string `json:"missing"`
	Differ  []string `json:"differ"`
	Extra   []string `json:"extra"`
}

// Clean reports whether the verification found no discrepancies.
func (v *VerifyResult) Clean() bool {
	return len(v.Missing) == 0 && len(v.Differ) == 0 && len(v.Extra) == 0
}

// Verify compares the local manifest against a reference (typically the
// last-transfer snapshot): files the reference has that the local tree lacks
// (missing), files whose size or mtime differ (differ), and local files the
// reference does not know about (extra).
func Verify(local, reference *Manifest) *VerifyResult {
	result := &VerifyResult{Root: local.Root, Checked: len(reference.Files)}
	localIdx := local.Index()
	refIdx := reference.Index()

	for _, f := range reference.Files {
		l, ok := localIdx[f.Path]
		if !ok {
			result.Missing = append(result.Missing, f.Path)
			continue
		}
		if l.Size != f.Size || l.MTime != f.MTime {
			result.Differ = append(result.Differ, f.Path)
		}
	}
	for _, f := range local.Files {
		if _, ok := refIdx[f.Path]; !ok {
			result.Extra = append(result.Extra, f.Path)
		}
	}
	return result
}

// EncodeJSON renders the manifest for --json consumers (and remote verify).
func (m *Manifest) EncodeJSON() ([]byte, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, camperrors.Wrap(err, "encode manifest")
	}
	return append(data, '\n'), nil
}

// DecodeManifest parses a manifest produced by EncodeJSON.
func DecodeManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &m); err != nil {
		return nil, camperrors.Wrap(err, "parse manifest")
	}
	return &m, nil
}
