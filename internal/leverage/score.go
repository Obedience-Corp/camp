package leverage

import (
	"context"
	"time"
)

// ElapsedMonths computes the number of months between start and end.
// Uses 30.44 days per month (average).
func ElapsedMonths(start, end time.Time) float64 {
	return end.Sub(start).Hours() / (24 * 30.44)
}

// SumLines totals the Lines and Code counts across all languages in an SCCResult.
func SumLines(result *SCCResult) (totalLines, totalCode int) {
	for _, lang := range result.LanguageSummary {
		totalLines += lang.Lines
		totalCode += lang.Code
	}
	return totalLines, totalCode
}

// SumFiles totals the file count across all languages in an SCCResult.
func SumFiles(result *SCCResult) int {
	var total int
	for _, lang := range result.LanguageSummary {
		total += lang.Count
	}
	return total
}

// ComputeScore calculates the leverage score for a single project.
//
// Formula:
//
//	FullLeverage   = (EstimatedPeople * EstimatedMonths) / (ActualPeople * ElapsedMonths)
//	SimpleLeverage = EstimatedPeople / ActualPeople
//
// scc provides EstimatedPeople directly in its json2 output — no need to derive it.
// If actualPeople or elapsedMonths is zero, leverage values are 0 (not NaN/Inf).
func ComputeScore(result *SCCResult, actualPeople int, elapsedMonths float64) *LeverageScore {
	totalLines, totalCode := SumLines(result)

	score := &LeverageScore{
		EstimatedPeople: result.EstimatedPeople,
		EstimatedMonths: result.EstimatedScheduleMonths,
		EstimatedCost:   result.EstimatedCost,
		ActualPeople:    float64(actualPeople),
		ElapsedMonths:   elapsedMonths,
		TotalFiles:      SumFiles(result),
		TotalLines:      totalLines,
		TotalCode:       totalCode,
	}

	if actualPeople > 0 {
		score.SimpleLeverage = result.EstimatedPeople / float64(actualPeople)
	}

	if actualPeople > 0 && elapsedMonths > 0 {
		estimatedPersonMonths := result.EstimatedPeople * result.EstimatedScheduleMonths
		actualPersonMonths := float64(actualPeople) * elapsedMonths
		score.FullLeverage = estimatedPersonMonths / actualPersonMonths
	}

	return score
}

// ProjectScoreParams configures project-level score policy.
type ProjectScoreParams struct {
	// AuthorFilter is the raw --author string (kept for diagnostics).
	AuthorFilter string
	// AuthorMatch is the expanded personal filter (emails + author IDs).
	// When Filter is non-empty, personal scoring is applied.
	AuthorMatch AuthorMatch
	// Resolver maps emails to canonical author IDs for ownership.
	Resolver *AuthorResolver
	// PeopleOverride forces team size when > 0.
	PeopleOverride int
	// FallbackElapsed is used when git date range is unavailable.
	FallbackElapsed float64
}

// ComputeProjectScore computes the leverage score for one resolved project.
//
// Personal mode (AuthorMatch.Filter set):
//   - Actual effort is the author's commit span in this project's GitDir only
//     (project row). Campaign aggregation must union spans across unique git
//     dirs — never sum these project actuals for the personal headline.
//   - Estimated effort is scaled by blame ownership fraction for the author.
func ComputeProjectScore(ctx context.Context, proj ResolvedProject, result *SCCResult, params ProjectScoreParams) *LeverageScore {
	var projActualPM float64
	var projPeople int
	var projElapsed float64

	if params.AuthorMatch.Filter != "" || params.AuthorFilter != "" {
		match := params.AuthorMatch
		if match.Filter == "" {
			match = ExpandAuthorFilter(params.Resolver, params.AuthorFilter)
		}
		projPeople = 1
		first, last, gitErr := AuthorDateRangeMatch(ctx, proj.GitDir, match)
		if gitErr == nil {
			projElapsed = ElapsedMonths(first, last)
		}
		if projElapsed <= 0 {
			projElapsed = minAuthorMonths
		}
		projActualPM = projElapsed

		score := ComputeScore(result, projPeople, projElapsed)
		score.ProjectName = proj.Name
		score.AuthorCount = proj.AuthorCount
		score.ActualPersonMonths = projActualPM

		ownership := AuthorOwnershipFraction(proj, match, params.Resolver)
		ScaleScoreForAuthor(score, ownership)
		if score.ActualPersonMonths > 0 {
			estPM := score.EstimatedPeople * score.EstimatedMonths
			score.FullLeverage = estPM / score.ActualPersonMonths
		}
		return score
	} else if params.PeopleOverride > 0 {
		projPeople = params.PeopleOverride
		first, last, gitErr := GitDateRange(ctx, proj.GitDir)
		if gitErr == nil {
			projElapsed = ElapsedMonths(first, last)
		}
		if projElapsed <= 0 {
			projElapsed = params.FallbackElapsed
		}
		projActualPM = float64(projPeople) * projElapsed
	} else if proj.ActualPersonMonths > 0 {
		projActualPM = proj.ActualPersonMonths
		projPeople = proj.AuthorCount
		if projPeople == 0 {
			projPeople = 1
		}
		first, last, gitErr := GitDateRange(ctx, proj.GitDir)
		if gitErr == nil {
			projElapsed = ElapsedMonths(first, last)
		}
		if projElapsed <= 0 {
			projElapsed = params.FallbackElapsed
		}
	} else {
		projPeople = proj.AuthorCount
		if projPeople == 0 {
			projPeople = 1
		}
		first, last, gitErr := GitDateRange(ctx, proj.GitDir)
		if gitErr == nil {
			projElapsed = ElapsedMonths(first, last)
		}
		if projElapsed <= 0 {
			projElapsed = params.FallbackElapsed
		}
		projActualPM = float64(projPeople) * projElapsed
	}

	score := ComputeScore(result, projPeople, projElapsed)
	score.ProjectName = proj.Name
	score.AuthorCount = proj.AuthorCount

	if projActualPM > 0 {
		score.ActualPersonMonths = projActualPM
		estPM := result.EstimatedPeople * result.EstimatedScheduleMonths
		score.FullLeverage = estPM / projActualPM
	}

	return score
}

// ComputePeriodScore calculates leverage for a single period between two
// consecutive snapshots. The period leverage measures new estimated effort
// delivered per actual person-month in the period.
//
// T1 and T2 are git commit dates — git is the source of truth for timing.
// If prev is nil (first snapshot), returns a score with IsFirst=true and
// zero leverage. If periodMonths or actualPeople is zero, leverage is zero.
func ComputePeriodScore(prev, current *LeverageScore, actualPeople int, periodMonths float64) *PeriodLeverageScore {
	score := &PeriodLeverageScore{
		PeriodMonths: periodMonths,
	}

	if prev == nil {
		score.IsFirst = true
		score.DeltaCode = current.TotalCode
		score.DeltaEstCost = current.EstimatedCost
		score.DeltaEstPersonMonths = current.EstimatedPeople * current.EstimatedMonths
		return score
	}

	prevPersonMonths := prev.EstimatedPeople * prev.EstimatedMonths
	currPersonMonths := current.EstimatedPeople * current.EstimatedMonths

	score.DeltaCode = current.TotalCode - prev.TotalCode
	score.DeltaEstCost = current.EstimatedCost - prev.EstimatedCost
	score.DeltaEstPersonMonths = currPersonMonths - prevPersonMonths
	score.IsNegative = score.DeltaEstPersonMonths < 0

	if actualPeople > 0 && periodMonths > 0 {
		actualPersonMonths := float64(actualPeople) * periodMonths
		score.PeriodLeverage = score.DeltaEstPersonMonths / actualPersonMonths
	}

	return score
}

// AggregateScores combines multiple per-project scores into a single
// campaign-wide score. It sums estimated person-months across all projects,
// then divides by actual person-months.
//
// Each score carries its own ActualPeople and ElapsedMonths, so per-project
// actual effort is summed correctly (different team sizes per project).
// The actualPeople and elapsedMonths params serve as fallbacks for scores
// that have zero ActualPeople (backward compat with history/snapshot callers).
func AggregateScores(scores []*LeverageScore, actualPeople int, elapsedMonths float64) *LeverageScore {
	var (
		totalEstimatedPersonMonths float64
		totalEstimatedPeople       float64
		totalEstimatedCost         float64
		totalActualPersonMonths    float64
		totalFiles                 int
		totalLines                 int
		totalCode                  int
		maxAuthors                 int
	)

	for _, s := range scores {
		totalEstimatedPersonMonths += s.EstimatedPeople * s.EstimatedMonths
		totalEstimatedPeople += s.EstimatedPeople
		totalEstimatedCost += s.EstimatedCost
		totalFiles += s.TotalFiles
		totalLines += s.TotalLines
		totalCode += s.TotalCode

		// Prefer contribution-based actual person-months when available.
		// This uses the sum of each author's individual active duration
		// rather than numAuthors × totalElapsed.
		if s.ActualPersonMonths > 0 {
			totalActualPersonMonths += s.ActualPersonMonths
		} else {
			ap := s.ActualPeople
			em := s.ElapsedMonths
			if ap > 0 && em > 0 {
				totalActualPersonMonths += ap * em
			}
		}

		if s.AuthorCount > maxAuthors {
			maxAuthors = s.AuthorCount
		}
	}

	// Fallback: if no per-project actual PM computed, use global params
	if totalActualPersonMonths == 0 && actualPeople > 0 && elapsedMonths > 0 {
		totalActualPersonMonths = float64(actualPeople) * elapsedMonths
	}

	agg := &LeverageScore{
		EstimatedPeople: totalEstimatedPeople,
		EstimatedCost:   totalEstimatedCost,
		ActualPeople:    float64(actualPeople),
		ElapsedMonths:   elapsedMonths,
		TotalFiles:      totalFiles,
		TotalLines:      totalLines,
		TotalCode:       totalCode,
		AuthorCount:     maxAuthors,
	}

	if totalActualPersonMonths > 0 {
		agg.FullLeverage = totalEstimatedPersonMonths / totalActualPersonMonths
		agg.ActualPersonMonths = totalActualPersonMonths

		if totalEstimatedPeople > 0 {
			agg.EstimatedMonths = totalEstimatedPersonMonths / totalEstimatedPeople
		}
	}

	if actualPeople > 0 {
		agg.SimpleLeverage = totalEstimatedPeople / float64(actualPeople)
	}

	return agg
}
