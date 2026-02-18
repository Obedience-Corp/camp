package leverage

import (
	"context"
	"strings"
)

// CampaignActualPersonMonths computes deduplicated, blame-weighted campaign-wide
// actual person-months across multiple projects. Merges authors by normalized
// name across git repos and weights each author's date span by their LOC share.
//
// Uses pre-populated Authors from PopulateProjectMetrics when available,
// avoiding redundant blame operations. Falls back to computing blame data
// when Authors is not populated.
//
// Authors with < 1% of total campaign LOC are excluded from effort calculation.
func CampaignActualPersonMonths(ctx context.Context, projects []ResolvedProject) (float64, error) {
	const minAuthorMonths = 0.1
	const minPercentage = 1.0

	if err := ctx.Err(); err != nil {
		return 0, err
	}

	// Phase 1: Merge date spans across unique git dirs by normalized author name.
	uniqueGitDirs := make(map[string]bool)
	for _, p := range projects {
		uniqueGitDirs[p.GitDir] = true
	}

	mergedSpans := make(map[string]*authorDateSpan)
	for gitDir := range uniqueGitDirs {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		authors, err := gitDirAuthors(ctx, gitDir)
		if err != nil {
			continue
		}

		for normName, info := range authors {
			span, ok := mergedSpans[normName]
			if !ok {
				span = &authorDateSpan{}
				mergedSpans[normName] = span
			}
			if span.earliest.IsZero() || info.earliest.Before(span.earliest) {
				span.earliest = info.earliest
			}
			if span.latest.IsZero() || info.latest.After(span.latest) {
				span.latest = info.latest
			}
		}
	}

	if len(mergedSpans) == 0 {
		return minAuthorMonths, nil
	}

	// Phase 2: Aggregate blame LOC across all projects by normalized author name.
	// Use pre-populated Authors when available (from PopulateProjectMetrics),
	// fall back to GetAuthorLOC only when Authors is nil.
	type authorLOC struct {
		lines int
		name  string
		email string
	}
	mergedLOC := make(map[string]*authorLOC)
	var totalLines int

	for _, p := range projects {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		var contribs []AuthorContribution
		if len(p.Authors) > 0 {
			contribs = p.Authors
		} else {
			var err error
			contribs, err = GetAuthorLOC(ctx, p.SCCDir)
			if err != nil {
				continue
			}
		}

		for _, c := range contribs {
			normName := strings.ToLower(strings.TrimSpace(c.Name))
			if a, ok := mergedLOC[normName]; ok {
				a.lines += c.Lines
			} else {
				mergedLOC[normName] = &authorLOC{lines: c.Lines, name: c.Name, email: c.Email}
			}
			totalLines += c.Lines
		}
	}

	// If no blame data, fall back to unweighted date-span PM.
	if totalLines == 0 {
		return dateSpanOnlyPM(mergedSpans, minAuthorMonths), nil
	}

	// Phase 3: Compute blame-weighted PM per qualifying author.
	var totalPM float64
	for normName, span := range mergedSpans {
		loc, ok := mergedLOC[normName]
		if !ok {
			continue
		}

		pct := float64(loc.lines) / float64(totalLines) * 100
		if pct < minPercentage {
			continue
		}

		if span.earliest.IsZero() {
			totalPM += minAuthorMonths * (pct / 100.0)
			continue
		}

		months := ElapsedMonths(span.earliest, span.latest)
		if months < minAuthorMonths {
			months = minAuthorMonths
		}

		totalPM += months * (pct / 100.0)
	}

	if totalPM < minAuthorMonths {
		totalPM = minAuthorMonths
	}
	return totalPM, nil
}

// dateSpanOnlyPM computes unweighted person-months from date spans only.
// Used as fallback when blame data is unavailable.
func dateSpanOnlyPM(dateSpans map[string]*authorDateSpan, minMonths float64) float64 {
	var totalPM float64
	for _, span := range dateSpans {
		if span.earliest.IsZero() {
			totalPM += minMonths
			continue
		}
		months := ElapsedMonths(span.earliest, span.latest)
		if months < minMonths {
			months = minMonths
		}
		totalPM += months
	}
	if totalPM < minMonths {
		totalPM = minMonths
	}
	return totalPM
}
