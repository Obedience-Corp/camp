package intent

import (
	"strings"
	"unicode"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// AllTypes returns every valid intent type.
func AllTypes() []Type {
	return []Type{TypeIdea, TypeFeature, TypeBug, TypeResearch, TypeChore, TypeFeedback}
}

// AllPriorities returns every valid priority, lowest first.
func AllPriorities() []Priority {
	return []Priority{PriorityLow, PriorityMedium, PriorityHigh}
}

// AllHorizons returns every valid horizon, soonest first.
func AllHorizons() []Horizon {
	return []Horizon{HorizonNow, HorizonNext, HorizonLater, HorizonSomeday}
}

// ParseType returns the intent type named by value.
func ParseType(value string) (Type, error) {
	t := Type(value)
	if !isValidType(t) {
		return "", invalidChoiceError("type", value, AllTypes())
	}
	return t, nil
}

// ParsePriority returns the priority named by value.
func ParsePriority(value string) (Priority, error) {
	p := Priority(value)
	if !isValidPriority(p) {
		return "", invalidChoiceError("priority", value, AllPriorities())
	}
	return p, nil
}

// ParseHorizon returns the horizon named by value.
func ParseHorizon(value string) (Horizon, error) {
	h := Horizon(value)
	if !isValidHorizon(h) {
		return "", invalidChoiceError("horizon", value, AllHorizons())
	}
	return h, nil
}

func invalidChoiceError[T ~string](field, value string, valid []T) error {
	names := make([]string, len(valid))
	for i, v := range valid {
		names[i] = string(v)
	}
	return camperrors.Wrapf(camperrors.ErrInvalidInput,
		"invalid %s %q (valid: %s)", field, value, strings.Join(names, ", "))
}

// NormalizeDependencyRefs trims each reference, removes duplicates while
// keeping order, and rejects references that are empty or span lines. field
// names the frontmatter key in the error.
func NormalizeDependencyRefs(field string, refs []string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	seen := make(map[string]bool, len(refs))
	out := make([]string, 0, len(refs))
	for _, raw := range refs {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "%s reference must not be empty", field)
		}
		if strings.ContainsFunc(ref, unicode.IsControl) {
			return nil, camperrors.Wrapf(camperrors.ErrInvalidInput,
				"%s reference %q must be a single line without control characters", field, ref)
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
	}
	return out, nil
}
