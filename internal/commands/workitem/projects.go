package workitem

import (
	"path/filepath"
	"strconv"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// normalizeProjects cleans each project path (filepath.Clean, forward slashes,
// no trailing separator), rejects any that normalize to empty (naming the
// original input), and deduplicates while preserving first-seen order.
func normalizeProjects(raw []string) ([]string, error) {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, p := range raw {
		norm := normalizeProject(p)
		if norm == "" {
			return nil, camperrors.NewValidation("project",
				"project "+strconv.Quote(p)+" is empty after normalization", nil)
		}
		if seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	return out, nil
}

func normalizeProject(s string) string {
	return strings.TrimRight(filepath.ToSlash(filepath.Clean(s)), "/")
}

// normalizeExistingProjects normalizes and deduplicates projects already on a
// marker. It returns the kept entries (normalized, deduped, first-occurrence
// order) and the original entries that normalized to empty (genuine drops that
// repair records distinctly as data removal). It never errors.
func normalizeExistingProjects(projects []string) (kept, dropped []string) {
	seen := make(map[string]bool, len(projects))
	for _, p := range projects {
		norm := normalizeProject(p)
		if norm == "" {
			dropped = append(dropped, p)
			continue
		}
		if seen[norm] {
			continue
		}
		seen[norm] = true
		kept = append(kept, norm)
	}
	return kept, dropped
}
