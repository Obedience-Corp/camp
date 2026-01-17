// Package fuzzy provides fuzzy matching for navigation targets.
package fuzzy

import (
	"sort"
	"strings"
)

// Match represents a fuzzy match result.
type Match struct {
	// Target is the matched string.
	Target string
	// Score is the match quality score (higher is better).
	Score int
	// Positions contains the indices of matched characters.
	Positions []int
}

// Matches is a sortable slice of Match results.
type Matches []Match

func (m Matches) Len() int           { return len(m) }
func (m Matches) Less(i, j int) bool { return m[i].Score > m[j].Score }
func (m Matches) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }

// Targets extracts target strings from matches.
func (m Matches) Targets() []string {
	targets := make([]string, len(m))
	for i, match := range m {
		targets[i] = match.Target
	}
	return targets
}

// Filter returns targets matching query, sorted by score.
// If query is empty, returns all targets with a default score.
func Filter(targets []string, query string) Matches {
	if query == "" {
		// Return all targets with default score
		matches := make(Matches, len(targets))
		for i, t := range targets {
			matches[i] = Match{Target: t, Score: 1}
		}
		return matches
	}

	var matches Matches
	for _, target := range targets {
		score, positions := Score(query, target)
		if score > 0 {
			matches = append(matches, Match{
				Target:    target,
				Score:     score,
				Positions: positions,
			})
		}
	}

	sort.Sort(matches)
	return matches
}

// FilterMulti handles space-separated query terms.
// All terms must match for a target to be included.
func FilterMulti(targets []string, query string) Matches {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return Filter(targets, "")
	}

	// Single term - use regular filter
	if len(terms) == 1 {
		return Filter(targets, terms[0])
	}

	// Each term must match
	remaining := targets
	for _, term := range terms {
		matches := Filter(remaining, term)
		if len(matches) == 0 {
			return nil
		}
		remaining = matches.Targets()
	}

	// Re-score with combined query for final ranking
	combinedQuery := strings.Join(terms, "")
	return Filter(remaining, combinedQuery)
}

// HasMatch returns true if query matches target.
func HasMatch(query, target string) bool {
	score, _ := Score(query, target)
	return score > 0
}
