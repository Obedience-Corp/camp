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
