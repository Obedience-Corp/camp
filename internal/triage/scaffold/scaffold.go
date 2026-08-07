// Package scaffold writes `.campaign/triage/` on first use: the commented
// profile, the per-type policies, and the OBEY.md guide.
//
// The embedded files are the source of truth for the shipped defaults. Spec
// doc 05 describes them; edits land here, not in the doc, because a default
// that lives only in prose drifts from the one users actually get.
package scaffold

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// all: is load-bearing. A bare //go:embed silently skips files whose names
// start with _ or . — which would have dropped types/_default.yaml, the
// policy every unknown workitem type inherits. The omission is silent at
// build time and only shows up as a type that triages with no vocabulary.
//
//go:embed all:files
var filesFS embed.FS

// DirName is the campaign-relative directory the scaffold writes into.
const DirName = ".campaign/triage"

// Status is what happened to one scaffolded file.
type Status string

const (
	// StatusCreated means the file did not exist and was written.
	StatusCreated Status = "created"
	// StatusUnchanged means the file exists and matches what camp ships.
	StatusUnchanged Status = "unchanged"
	// StatusDiverged means the file exists and differs from the shipped
	// version. It is left exactly as the user wrote it.
	StatusDiverged Status = "diverged"
)

// FileResult is one file's outcome.
type FileResult struct {
	// Path is campaign-relative.
	Path   string `json:"path"`
	Status Status `json:"status"`
}

// Result is a whole scaffold pass.
type Result struct {
	Files []FileResult `json:"files"`
}

// Created returns the paths this pass wrote.
func (r Result) Created() []string { return r.pathsWith(StatusCreated) }

// Diverged returns the paths that exist but differ from what camp ships.
func (r Result) Diverged() []string { return r.pathsWith(StatusDiverged) }

func (r Result) pathsWith(status Status) []string {
	var out []string
	for _, file := range r.Files {
		if file.Status == status {
			out = append(out, file.Path)
		}
	}
	return out
}

// Wrote reports whether this pass created anything.
func (r Result) Wrote() bool { return len(r.Created()) > 0 }

// Ensure writes any missing scaffold file under campaignRoot and reports what
// it found.
//
// An existing file is never overwritten, and a file that differs from the
// shipped version is reported rather than replaced or refused. The profile is
// meant to be edited — that is the whole point of scaffolding it commented
// rather than leaving an empty file inheriting invisible defaults. Divergence
// is information, not an error, so it travels back as a status and the caller
// decides whether to mention it.
func Ensure(ctx context.Context, campaignRoot string) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	names, err := embeddedNames()
	if err != nil {
		return nil, err
	}

	result := &Result{}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		relPath := filepath.Join(DirName, filepath.FromSlash(name))
		abs := filepath.Join(campaignRoot, relPath)

		want, err := filesFS.ReadFile(path(name))
		if err != nil {
			return nil, camperrors.Wrapf(err, "reading embedded %s", name)
		}

		status, err := ensureFile(abs, want)
		if err != nil {
			return nil, err
		}
		result.Files = append(result.Files, FileResult{
			Path: filepath.ToSlash(relPath), Status: status,
		})
	}
	return result, nil
}

// ensureFile writes want to abs when nothing is there, and otherwise reports
// whether what is there matches.
func ensureFile(abs string, want []byte) (Status, error) {
	got, err := os.ReadFile(abs)
	if err == nil {
		if Digest(got) == Digest(want) {
			return StatusUnchanged, nil
		}
		return StatusDiverged, nil
	}
	if !os.IsNotExist(err) {
		return "", camperrors.Wrapf(err, "reading %s", abs)
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", camperrors.Wrapf(err, "creating %s", filepath.Dir(abs))
	}
	if err := fsutil.WriteFileAtomically(abs, want, 0o644); err != nil {
		return "", err
	}
	return StatusCreated, nil
}

// Digest is the content hash divergence is decided by. Exported so a test can
// state "this file is byte-identical to what camp ships" in the same terms the
// scaffold does.
func Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// File returns one shipped file's contents by its scaffold-relative name,
// e.g. "profile.yaml" or "types/design.yaml".
func File(name string) ([]byte, error) {
	body, err := filesFS.ReadFile(path(name))
	if err != nil {
		return nil, camperrors.Wrapf(err, "no scaffold file named %q", name)
	}
	return body, nil
}

// Names lists every shipped file, in stable order.
func Names() ([]string, error) { return embeddedNames() }

// embeddedNames walks the embedded tree.
func embeddedNames() ([]string, error) {
	var names []string
	err := fs.WalkDir(filesFS, "files", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		names = append(names, strings.TrimPrefix(p, "files/"))
		return nil
	})
	if err != nil {
		return nil, camperrors.Wrap(err, "walking the embedded scaffold")
	}
	sort.Strings(names)
	return names, nil
}

// path is the embedded path for a scaffold-relative name.
func path(name string) string { return "files/" + name }
