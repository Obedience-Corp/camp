package triage

import (
	"path"
	"strings"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

// Scope selects which workitems a run considers. It is a thin composition of
// the profile's scope block and any `--scope` expressions, expressed as the
// same `workitem.FilterOptions` that `camp workitem` uses.
//
// The type deliberately owns no matching logic of its own beyond path globs:
// scope semantics that drift from what `camp workitem --type design` shows
// would make the two commands disagree about what a campaign contains.
type Scope struct {
	// Filter is handed to workitem.FilterAdvanced unchanged.
	Filter workitem.FilterOptions
	// ExcludeTypes drops whole workflow types (profile `scope.exclude_types`).
	ExcludeTypes []string
	// ExcludePaths are campaign-relative globs, matched with path.Match
	// against each item's relative path.
	ExcludePaths []string
	// IncludePaths narrows the run to items under the given globs. Empty
	// means no path restriction; `--scope path:...` fills it.
	IncludePaths []string
}

// ScopeKeys returns the expression keys `--scope` accepts, in help order.
func ScopeKeys() []string {
	return []string{
		"type", "category", "status", "stage", "attention-stage",
		"group", "tag", "project", "query", "path",
	}
}

// NewScope builds the scope for a run from its resolved profile.
func NewScope(profile ResolvedProfile) Scope {
	return Scope{
		Filter:       workitem.FilterOptions{ShowParked: profile.Scope.IncludeParked},
		ExcludeTypes: append([]string(nil), profile.Scope.ExcludeTypes...),
		ExcludePaths: append([]string(nil), profile.Scope.ExcludePaths...),
	}
}

// ApplyExpressions layers `--scope key:value` expressions onto the scope.
//
// It reports every malformed expression at once rather than stopping at the
// first, matching how the rest of triage rejects input: one run, one list of
// what to fix.
func (s *Scope) ApplyExpressions(exprs []string) error {
	var violations []Violation
	for i, raw := range exprs {
		field := indexPath("scope", i)
		expr := strings.TrimSpace(raw)
		key, value, ok := strings.Cut(expr, ":")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			violations = append(violations, Violation{
				Field:   field,
				Message: "expected key:value, got " + quote(raw),
				Allowed: ScopeKeys(),
			})
			continue
		}
		if v := s.applyOne(key, value); v != nil {
			v.Field = field
			violations = append(violations, *v)
		}
	}
	return newValidationError("scope expression", violations)
}

// applyOne routes one expression to the filter field it names.
func (s *Scope) applyOne(key, value string) *Violation {
	switch key {
	case "type":
		s.Filter.Types = append(s.Filter.Types, value)
	case "category":
		s.Filter.Categories = append(s.Filter.Categories, value)
	case "status":
		s.Filter.Statuses = append(s.Filter.Statuses, value)
	case "stage":
		s.Filter.LifecycleStages = append(s.Filter.LifecycleStages, value)
	case "attention-stage", "attention_stage":
		s.Filter.AttentionStages = append(s.Filter.AttentionStages, value)
	case "group":
		s.Filter.Groups = append(s.Filter.Groups, value)
	case "tag":
		s.Filter.Tags = append(s.Filter.Tags, value)
	case "project":
		s.Filter.Projects = append(s.Filter.Projects, value)
	case "query":
		s.Filter.Query = value
	case "path":
		if _, err := path.Match(value, ""); err != nil {
			return &Violation{Message: "invalid path glob " + quote(value) + ": " + err.Error()}
		}
		s.IncludePaths = append(s.IncludePaths, value)
	default:
		return &Violation{
			Message: "unknown scope key " + quote(key),
			Allowed: ScopeKeys(),
		}
	}
	return nil
}

// Apply returns the items in scope, in the order Discover produced them.
func (s Scope) Apply(items []workitem.WorkItem) []workitem.WorkItem {
	filtered := workitem.FilterAdvanced(items, s.Filter)

	excludedTypes := make(map[string]bool, len(s.ExcludeTypes))
	for _, t := range s.ExcludeTypes {
		excludedTypes[t] = true
	}

	out := make([]workitem.WorkItem, 0, len(filtered))
	for _, item := range filtered {
		if excludedTypes[string(item.WorkflowType)] {
			continue
		}
		if matchesAnyGlob(item.RelativePath, s.ExcludePaths) {
			continue
		}
		if len(s.IncludePaths) > 0 && !matchesAnyGlob(item.RelativePath, s.IncludePaths) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// matchesAnyGlob reports whether relPath matches any pattern. A pattern also
// matches everything beneath it, so `projects` excludes `projects/camp/...`
// without the caller having to write a trailing wildcard.
func matchesAnyGlob(relPath string, patterns []string) bool {
	clean := path.Clean(strings.TrimPrefix(relPath, "./"))
	for _, pattern := range patterns {
		pattern = strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/")
		if pattern == "" {
			continue
		}
		if ok, err := path.Match(pattern, clean); err == nil && ok {
			return true
		}
		if strings.HasPrefix(clean, pattern+"/") {
			return true
		}
		if ok, err := path.Match(pattern+"/*", clean); err == nil && ok {
			return true
		}
	}
	return false
}
