package triage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// LoadRenderInput gathers everything the renderers need from a run.
//
// Reading is tolerant on purpose: a malformed rationale or evidence record
// must not make the review document unrenderable. The document is how an
// operator sees the run, so it has to survive one bad file inside it.
func LoadRenderInput(ctx context.Context, store *Store, runID string) (RenderInput, error) {
	if err := ctx.Err(); err != nil {
		return RenderInput{}, err
	}
	run, err := store.OpenRun(ctx, runID)
	if err != nil {
		return RenderInput{}, err
	}
	verdicts, err := store.Verdicts(ctx, runID)
	if err != nil {
		return RenderInput{}, err
	}

	in := RenderInput{
		Run:           run,
		Verdicts:      verdicts,
		Rationales:    map[string]*Rationale{},
		EvidenceRoles: map[EvidenceRole]int{},
	}

	for _, row := range run.Manifest.Rows {
		if err := ctx.Err(); err != nil {
			return RenderInput{}, err
		}
		if record, err := store.Evidence(ctx, runID, row.StableID); err == nil && record != nil {
			if record.NoEvidence {
				in.NoEvidenceCount++
			} else {
				in.EvidenceRoles[record.ProducedBy.Role]++
			}
		}
		if rationale, err := store.Rationale(ctx, runID, row.StableID); err == nil && rationale != nil {
			in.Rationales[row.StableID] = rationale
		}
	}
	return in, nil
}

// Rationale loads a row's recorded rationale, or nil when it has none.
func (s *Store) Rationale(ctx context.Context, runID, stableID string) (*Rationale, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.RunDir(runID), RationaleDirName, recordFileName(stableID))
	var rationale Rationale
	if err := s.readDocument(path, &rationale); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &rationale, nil
}

// RenderResult reports what a render wrote.
type RenderResult struct {
	RunID          string `json:"run_id"`
	ReviewPath     string `json:"review_path"`
	PrioritiesPath string `json:"priorities_path"`
	Rows           int    `json:"rows"`
	Lanes          int    `json:"lanes"`
}

// RenderDocuments renders both documents into the run directory.
//
// Documents are regenerated wholesale rather than patched: they are output,
// and deleting one loses nothing.
func (s *Store) RenderDocuments(ctx context.Context, runID string) (*RenderResult, error) {
	in, err := LoadRenderInput(ctx, s, runID)
	if err != nil {
		return nil, err
	}

	reviewPath := filepath.Join(s.RunDir(runID), ReviewDocFileName)
	if err := s.writeLocked(ctx, reviewPath, RenderReview(in)); err != nil {
		return nil, err
	}
	prioritiesPath := filepath.Join(s.RunDir(runID), PrioritiesDocFileName)
	if err := s.writeLocked(ctx, prioritiesPath, RenderPriorities(in)); err != nil {
		return nil, err
	}

	return &RenderResult{
		RunID:          runID,
		ReviewPath:     reviewPath,
		PrioritiesPath: prioritiesPath,
		Rows:           len(in.Run.Manifest.Rows),
		Lanes:          len(BuildLanes(in.Run, in.Verdicts, in.Rationales)),
	}, nil
}

// ExportPriorities writes the rendered priorities brief to the profile's
// export path and returns where it landed.
//
// The export is a copy at a path the user already looks at; the run stays the
// source of truth. It overwrites rather than versioning (D3): a versioned
// export would recreate the stale-priorities-doc problem triage exists to end.
// An empty export path is not an error — it means the campaign asked for no
// copy — and the caller reports that rather than failing.
func (s *Store) ExportPriorities(ctx context.Context, campaignRoot, runID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	run, err := s.OpenRun(ctx, runID)
	if err != nil {
		return "", err
	}
	rel := strings.TrimSpace(run.Manifest.Profile.Resolved.Outputs.PrioritiesExport)
	if rel == "" {
		return "", nil
	}
	abs, err := ResolveExportPath(campaignRoot, rel)
	if err != nil {
		return "", err
	}

	in, err := LoadRenderInput(ctx, s, runID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), dirMode); err != nil {
		return "", camperrors.Wrapf(err, "create export directory for %s", rel)
	}
	if err := s.writeLocked(ctx, abs, RenderPriorities(in)); err != nil {
		return "", err
	}
	return abs, nil
}

// ResolveExportPath turns a profile's campaign-relative export path into an
// absolute one, refusing anything that would write outside the campaign.
//
// The profile is a file a user edits, so this is untrusted input reaching a
// write. An absolute path or one climbing out with `..` is refused by name
// rather than silently clamped, because a silently relocated export would be a
// brief the user never finds.
func ResolveExportPath(campaignRoot, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", camperrors.NewValidation("outputs.priorities_export", "is empty", nil)
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", camperrors.NewValidation("outputs.priorities_export",
			"must be camp-relative, got the absolute path "+rel, camperrors.ErrInvalidInput)
	}
	clean := path.Clean(filepath.ToSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", camperrors.NewValidation("outputs.priorities_export",
			"must stay inside the camp, got "+rel, camperrors.ErrInvalidInput)
	}

	abs := filepath.Join(campaignRoot, filepath.FromSlash(clean))
	if err := pathutil.ValidateBoundary(campaignRoot, abs); err != nil {
		return "", camperrors.NewValidation("outputs.priorities_export",
			rel+" resolves outside the camp root", err)
	}
	return abs, nil
}
