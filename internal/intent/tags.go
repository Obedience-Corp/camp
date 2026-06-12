package intent

import (
	"strings"
	"unicode"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// validateAndNormalizeTags trims each tag, drops empties, de-duplicates while
// preserving order, and rejects any tag whose characters would corrupt the
// flow-style YAML the templates render (tags: [a, b, ...]). Without this guard a
// tag containing a comma silently splits into two tags and a tag containing a
// colon makes the frontmatter unparseable. Tags may use letters, numbers,
// spaces, and the separators - _ / . + ; anything else is rejected here, at the
// single entry point every create/update path funnels through.
func validateAndNormalizeTags(tags []string) ([]string, error) {
	if len(tags) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		for _, r := range t {
			if !validTagRune(r) {
				return nil, camperrors.Wrapf(camperrors.ErrInvalidInput,
					"tag %q contains unsupported character %q (allowed: letters, numbers, spaces, and - _ / . +)", t, string(r))
			}
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out, nil
}

// validTagRune reports whether r is allowed in a tag. The set is deliberately
// narrow so a rendered tag is always a valid YAML flow scalar.
func validTagRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsNumber(r) || r == ' ' {
		return true
	}
	switch r {
	case '-', '_', '/', '.', '+':
		return true
	}
	return false
}
