package workitem

import (
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func applyIncludes(repoRoot string, stage, includes []string) ([]string, []SkippedEntry, error) {
	if len(includes) == 0 {
		return stage, nil, nil
	}
	out := append([]string{}, stage...)
	var skip []SkippedEntry
	for _, inc := range includes {
		rel, err := relativeToRepo(repoRoot, inc)
		if err != nil {
			skip = append(skip, SkippedEntry{Path: inc, Reason: skipReasonOutOfScope})
			continue
		}
		out = append(out, rel)
	}
	return out, skip, nil
}

func applyExcludes(stage []string, excludes []string, skip *[]SkippedEntry) []string {
	if len(excludes) == 0 {
		return stage
	}
	ex := make(map[string]bool, len(excludes))
	for _, e := range excludes {
		ex[filepath.ToSlash(e)] = true
	}
	kept := make([]string, 0, len(stage))
	for _, p := range stage {
		if ex[filepath.ToSlash(p)] {
			*skip = append(*skip, SkippedEntry{Path: p, Reason: skipReasonExcludeFlag})
			continue
		}
		kept = append(kept, p)
	}
	return kept
}

func relativeToRepo(repoRoot, p string) (string, error) {
	if !filepath.IsAbs(p) {
		// Treat as already-repo-relative; reject escape attempts.
		clean := filepath.Clean(p)
		if strings.HasPrefix(clean, "..") {
			return "", camperrors.NewValidation("include", "path escapes repo root: "+p, nil)
		}
		return filepath.ToSlash(clean), nil
	}
	rel, err := filepath.Rel(repoRoot, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", camperrors.NewValidation("include", "path outside repo: "+p, nil)
	}
	return filepath.ToSlash(rel), nil
}

func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, p := range in {
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
