package index

import (
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/nav/fuzzy"
)

// CompletionCandidate represents a single completion suggestion with metadata.
type CompletionCandidate struct {
	Name     string // Target name
	Path     string // Relative path from campaign root
	Category string // Target category (e.g., "project", "festival", etc.)
	Score    int    // Fuzzy match score (higher is better)
}

// Query provides index query operations.
type Query struct {
	index *Index
}

// NewQuery creates a query interface for an index.
func NewQuery(idx *Index) *Query {
	return &Query{index: idx}
}

// All returns all targets in the index.
func (q *Query) All() []Target {
	if q.index == nil {
		return nil
	}
	return q.index.Targets
}

// Count returns the number of targets in the index.
func (q *Query) Count() int {
	if q.index == nil {
		return 0
	}
	return len(q.index.Targets)
}

// ByCategory returns targets in a specific category.
// If cat is CategoryAll, returns all targets.
func (q *Query) ByCategory(cat nav.Category) []Target {
	if q.index == nil {
		return nil
	}

	if cat == nav.CategoryAll {
		return q.All()
	}

	results := make([]Target, 0)
	for _, t := range q.index.Targets {
		if t.Category == cat {
			results = append(results, t)
		}
	}
	return results
}

// Categories returns all unique categories present in the index.
func (q *Query) Categories() []nav.Category {
	if q.index == nil {
		return nil
	}

	seen := make(map[nav.Category]bool)
	cats := make([]nav.Category, 0)

	for _, t := range q.index.Targets {
		if !seen[t.Category] {
			seen[t.Category] = true
			cats = append(cats, t.Category)
		}
	}
	return cats
}

// Search performs fuzzy search on targets.
// Returns targets matching the query, filtered by category.
func (q *Query) Search(query string, cat nav.Category) []Target {
	targets := q.ByCategory(cat)

	if query == "" {
		return targets
	}

	// Convert to string slice for fuzzy matching
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = t.Name
	}

	// Fuzzy filter
	matches := fuzzy.Filter(names, query)

	// Build result from matches (preserving fuzzy match order)
	results := make([]Target, 0, len(matches))
	for _, m := range matches {
		for _, t := range targets {
			if t.Name == m.Target {
				results = append(results, t)
				break
			}
		}
	}

	return results
}

// Complete returns completion candidates for a prefix.
// Matches targets whose name starts with the prefix (case-insensitive).
func (q *Query) Complete(prefix string, cat nav.Category) []string {
	targets := q.ByCategory(cat)

	candidates := make([]string, 0)
	prefixLower := strings.ToLower(prefix)

	for _, t := range targets {
		nameLower := strings.ToLower(t.Name)
		if strings.HasPrefix(nameLower, prefixLower) {
			candidates = append(candidates, t.Name)
		}
	}

	return candidates
}

// CompleteAny returns completion candidates matching anywhere in the name.
// Matches targets whose name contains the partial string (case-insensitive).
func (q *Query) CompleteAny(partial string, cat nav.Category) []string {
	targets := q.ByCategory(cat)

	candidates := make([]string, 0)
	partialLower := strings.ToLower(partial)

	for _, t := range targets {
		nameLower := strings.ToLower(t.Name)
		if strings.Contains(nameLower, partialLower) {
			candidates = append(candidates, t.Name)
		}
	}

	return candidates
}

// FuzzyComplete returns completion candidates using fuzzy matching.
// If cat is not CategoryAll, only targets in that category are considered.
// Results are sorted by fuzzy match score (highest first).
func (q *Query) FuzzyComplete(query string, cat nav.Category) []CompletionCandidate {
	targets := q.ByCategory(cat)

	names := make([]string, len(targets))
	nameToTarget := make(map[string]*Target, len(targets))
	for i := range targets {
		names[i] = targets[i].Name
		nameToTarget[targets[i].Name] = &targets[i]
	}

	matches := fuzzy.Filter(names, query)

	candidates := make([]CompletionCandidate, 0, len(matches))
	for _, m := range matches {
		target := nameToTarget[m.Target]
		candidates = append(candidates, CompletionCandidate{
			Name:     target.Name,
			Path:     target.RelativePath(q.index.CampaignRoot),
			Category: string(target.Category),
			Score:    m.Score,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	return candidates
}

// Find returns exact match by name.
// Returns nil if not found.
func (q *Query) Find(name string) *Target {
	if q.index == nil {
		return nil
	}

	for i := range q.index.Targets {
		if q.index.Targets[i].Name == name {
			return &q.index.Targets[i]
		}
	}
	return nil
}

// FindInCategory returns exact match by name within a category.
// Returns nil if not found.
func (q *Query) FindInCategory(name string, cat nav.Category) *Target {
	if q.index == nil {
		return nil
	}

	for i := range q.index.Targets {
		t := &q.index.Targets[i]
		if t.Name == name && (cat == nav.CategoryAll || t.Category == cat) {
			return t
		}
	}
	return nil
}

// Names returns the names of all targets.
func (q *Query) Names() []string {
	if q.index == nil {
		return nil
	}

	names := make([]string, len(q.index.Targets))
	for i, t := range q.index.Targets {
		names[i] = t.Name
	}
	return names
}

// NamesByCategory returns the names of targets in a category.
func (q *Query) NamesByCategory(cat nav.Category) []string {
	targets := q.ByCategory(cat)

	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = t.Name
	}
	return names
}

// Paths returns the paths of all targets.
func (q *Query) Paths() []string {
	if q.index == nil {
		return nil
	}

	paths := make([]string, len(q.index.Targets))
	for i, t := range q.index.Targets {
		paths[i] = t.Path
	}
	return paths
}
