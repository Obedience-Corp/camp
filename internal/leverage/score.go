package leverage

import (
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

// AggregateScores combines multiple per-project scores into a single
// campaign-wide score. It sums estimated person-months across all projects,
// then divides by actual person-months.
func AggregateScores(scores []*LeverageScore, actualPeople int, elapsedMonths float64) *LeverageScore {
	var (
		totalEstimatedPersonMonths float64
		totalEstimatedPeople       float64
		totalEstimatedCost         float64
		totalLines                 int
		totalCode                  int
	)

	for _, s := range scores {
		totalEstimatedPersonMonths += s.EstimatedPeople * s.EstimatedMonths
		totalEstimatedPeople += s.EstimatedPeople
		totalEstimatedCost += s.EstimatedCost
		totalLines += s.TotalLines
		totalCode += s.TotalCode
	}

	agg := &LeverageScore{
		EstimatedPeople: totalEstimatedPeople,
		EstimatedCost:   totalEstimatedCost,
		ActualPeople:    float64(actualPeople),
		ElapsedMonths:   elapsedMonths,
		TotalLines:      totalLines,
		TotalCode:       totalCode,
	}

	if actualPeople > 0 {
		agg.SimpleLeverage = totalEstimatedPeople / float64(actualPeople)
	}

	if actualPeople > 0 && elapsedMonths > 0 {
		actualPersonMonths := float64(actualPeople) * elapsedMonths
		agg.FullLeverage = totalEstimatedPersonMonths / actualPersonMonths

		// EstimatedMonths for aggregate: weighted average
		if totalEstimatedPeople > 0 {
			agg.EstimatedMonths = totalEstimatedPersonMonths / totalEstimatedPeople
		}
	}

	return agg
}
