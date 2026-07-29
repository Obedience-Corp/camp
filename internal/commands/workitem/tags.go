package workitem

import (
	"strconv"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// normalizeTags lowercases and hyphenates each input tag, rejects any that
// still fail validation after normalization, and deduplicates while preserving
// first-seen order.
func normalizeTags(raw []string) ([]string, error) {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		norm := normalizeTag(t)
		if !wkitem.ValidTag(norm) {
			return nil, camperrors.NewValidation("tag",
				"tag "+strconv.Quote(t)+" is not a valid tag after normalization (want lowercase kebab-case, got "+strconv.Quote(norm)+")", nil)
		}
		if seen[norm] {
			continue
		}
		seen[norm] = true
		out = append(out, norm)
	}
	return out, nil
}

func normalizeTag(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '_':
			return '-'
		default:
			return r
		}
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// normalizeExistingTags normalizes and deduplicates tags already on a marker.
// It returns the kept tags (normalized, deduped, first-occurrence order) and the
// original entries that normalized to empty (genuine drops that repair records
// distinctly as data removal). It never errors.
func normalizeExistingTags(tags []string) (kept, dropped []string) {
	seen := make(map[string]bool, len(tags))
	for _, t := range tags {
		norm := normalizeTag(t)
		if norm == "" {
			dropped = append(dropped, t)
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
