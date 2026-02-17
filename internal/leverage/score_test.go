package leverage

import (
	"math"
	"testing"
	"time"
)

func TestElapsedMonths(t *testing.T) {
	tests := []struct {
		name       string
		start, end time.Time
		wantApprox float64
	}{
		{
			name:       "30 days",
			start:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC),
			wantApprox: 0.986, // 30 days / 30.44
		},
		{
			name:       "365 days",
			start:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			wantApprox: 11.99, // 365 / 30.44
		},
		{
			name:       "zero",
			start:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			end:        time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			wantApprox: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ElapsedMonths(tt.start, tt.end)
			if math.Abs(got-tt.wantApprox) > 0.02 {
				t.Errorf("ElapsedMonths = %f, want ~%f", got, tt.wantApprox)
			}
		})
	}
}

func TestSumLines(t *testing.T) {
	result := &SCCResult{
		LanguageSummary: []LanguageEntry{
			{Lines: 1000, Code: 800},
			{Lines: 500, Code: 400},
		},
	}
	totalLines, totalCode := SumLines(result)
	if totalLines != 1500 {
		t.Errorf("totalLines: want 1500, got %d", totalLines)
	}
	if totalCode != 1200 {
		t.Errorf("totalCode: want 1200, got %d", totalCode)
	}
}

func TestComputeScore(t *testing.T) {
	tests := []struct {
		name          string
		result        *SCCResult
		actualPeople  int
		elapsedMonths float64
		wantFull      float64
		wantSimple    float64
	}{
		{
			name: "basic",
			result: &SCCResult{
				EstimatedPeople:         10.0,
				EstimatedScheduleMonths: 5.0,
				EstimatedCost:           100000,
				LanguageSummary:         []LanguageEntry{{Lines: 1000, Code: 800}},
			},
			actualPeople:  1,
			elapsedMonths: 2.0,
			wantFull:      25.0, // (10 * 5) / (1 * 2) = 50/2 = 25
			wantSimple:    10.0, // 10 / 1 = 10
		},
		{
			name: "two people",
			result: &SCCResult{
				EstimatedPeople:         10.0,
				EstimatedScheduleMonths: 5.0,
			},
			actualPeople:  2,
			elapsedMonths: 5.0,
			wantFull:      5.0, // (10 * 5) / (2 * 5) = 50/10 = 5
			wantSimple:    5.0, // 10 / 2 = 5
		},
		{
			name: "zero people",
			result: &SCCResult{
				EstimatedPeople:         10.0,
				EstimatedScheduleMonths: 5.0,
			},
			actualPeople:  0,
			elapsedMonths: 2.0,
			wantFull:      0.0,
			wantSimple:    0.0,
		},
		{
			name: "zero months",
			result: &SCCResult{
				EstimatedPeople:         10.0,
				EstimatedScheduleMonths: 5.0,
			},
			actualPeople:  1,
			elapsedMonths: 0.0,
			wantFull:      0.0,
			wantSimple:    10.0, // simple leverage still works
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ComputeScore(tt.result, tt.actualPeople, tt.elapsedMonths)
			if math.Abs(score.FullLeverage-tt.wantFull) > 0.01 {
				t.Errorf("FullLeverage: want %f, got %f", tt.wantFull, score.FullLeverage)
			}
			if math.Abs(score.SimpleLeverage-tt.wantSimple) > 0.01 {
				t.Errorf("SimpleLeverage: want %f, got %f", tt.wantSimple, score.SimpleLeverage)
			}
		})
	}
}

func TestComputePeriodScore(t *testing.T) {
	tests := []struct {
		name           string
		prev           *LeverageScore
		current        *LeverageScore
		actualPeople   int
		periodMonths   float64
		wantLeverage   float64
		wantDeltaCode  int
		wantIsFirst    bool
		wantIsNegative bool
	}{
		{
			name: "normal period — design spec worked example",
			prev: &LeverageScore{
				EstimatedPeople: 9.5, EstimatedMonths: 45.0, // ~428 person-months
				TotalCode: 437954, EstimatedCost: 10000000,
			},
			current: &LeverageScore{
				EstimatedPeople: 14.3, EstimatedMonths: 36.2, // ~518 person-months
				TotalCode: 500292, EstimatedCost: 12000000,
			},
			actualPeople: 1,
			periodMonths: 0.23, // 1 week
			// Delta = (14.3*36.2) - (9.5*45.0) = 517.66 - 427.5 = 90.16
			// Leverage = 90.16 / (1 * 0.23) ≈ 392
			wantLeverage:   392.0,
			wantDeltaCode:  62338,
			wantIsFirst:    false,
			wantIsNegative: false,
		},
		{
			name: "idle period — zero delta",
			prev: &LeverageScore{
				EstimatedPeople: 14.0, EstimatedMonths: 36.0,
				TotalCode: 528822, EstimatedCost: 11000000,
			},
			current: &LeverageScore{
				EstimatedPeople: 14.0, EstimatedMonths: 36.0,
				TotalCode: 528822, EstimatedCost: 11000000,
			},
			actualPeople:   1,
			periodMonths:   0.23,
			wantLeverage:   0.0,
			wantDeltaCode:  0,
			wantIsFirst:    false,
			wantIsNegative: false,
		},
		{
			name:           "first period — no prior snapshot",
			prev:           nil,
			current:        &LeverageScore{EstimatedPeople: 5.0, EstimatedMonths: 10.0, TotalCode: 10000, EstimatedCost: 500000},
			actualPeople:   1,
			periodMonths:   1.0,
			wantLeverage:   0.0, // first period has no leverage
			wantDeltaCode:  10000,
			wantIsFirst:    true,
			wantIsNegative: false,
		},
		{
			name: "negative delta — code removal",
			prev: &LeverageScore{
				EstimatedPeople: 10.0, EstimatedMonths: 20.0,
				TotalCode: 100000, EstimatedCost: 5000000,
			},
			current: &LeverageScore{
				EstimatedPeople: 8.0, EstimatedMonths: 18.0,
				TotalCode: 80000, EstimatedCost: 4000000,
			},
			actualPeople: 1,
			periodMonths: 0.23,
			// Delta = (8*18) - (10*20) = 144 - 200 = -56
			// Leverage = -56 / 0.23 ≈ -243.5
			wantLeverage:   -243.5,
			wantDeltaCode:  -20000,
			wantIsFirst:    false,
			wantIsNegative: true,
		},
		{
			name: "zero period length",
			prev: &LeverageScore{
				EstimatedPeople: 5.0, EstimatedMonths: 10.0,
				TotalCode: 50000,
			},
			current: &LeverageScore{
				EstimatedPeople: 6.0, EstimatedMonths: 11.0,
				TotalCode: 60000,
			},
			actualPeople:   1,
			periodMonths:   0.0,
			wantLeverage:   0.0,
			wantDeltaCode:  10000,
			wantIsFirst:    false,
			wantIsNegative: false,
		},
		{
			name: "zero actual people",
			prev: &LeverageScore{
				EstimatedPeople: 5.0, EstimatedMonths: 10.0,
				TotalCode: 50000,
			},
			current: &LeverageScore{
				EstimatedPeople: 6.0, EstimatedMonths: 11.0,
				TotalCode: 60000,
			},
			actualPeople:   0,
			periodMonths:   1.0,
			wantLeverage:   0.0,
			wantDeltaCode:  10000,
			wantIsFirst:    false,
			wantIsNegative: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ComputePeriodScore(tt.prev, tt.current, tt.actualPeople, tt.periodMonths)

			if score.IsFirst != tt.wantIsFirst {
				t.Errorf("IsFirst = %v, want %v", score.IsFirst, tt.wantIsFirst)
			}
			if score.IsNegative != tt.wantIsNegative {
				t.Errorf("IsNegative = %v, want %v", score.IsNegative, tt.wantIsNegative)
			}
			if score.DeltaCode != tt.wantDeltaCode {
				t.Errorf("DeltaCode = %d, want %d", score.DeltaCode, tt.wantDeltaCode)
			}
			if math.Abs(score.PeriodLeverage-tt.wantLeverage) > 1.0 {
				t.Errorf("PeriodLeverage = %.1f, want ~%.1f", score.PeriodLeverage, tt.wantLeverage)
			}
		})
	}
}

func TestAggregateScores(t *testing.T) {
	scores := []*LeverageScore{
		{
			EstimatedPeople: 5.0,
			EstimatedMonths: 4.0,
			EstimatedCost:   50000,
			TotalLines:      1000,
			TotalCode:       800,
		},
		{
			EstimatedPeople: 3.0,
			EstimatedMonths: 6.0,
			EstimatedCost:   75000,
			TotalLines:      2000,
			TotalCode:       1600,
		},
	}

	agg := AggregateScores(scores, 1, 10.0)

	// Total estimated person-months = (5*4) + (3*6) = 20 + 18 = 38
	// Actual person-months = 1 * 10 = 10 (fallback since scores have no ActualPeople)
	// FullLeverage = 38 / 10 = 3.8
	if math.Abs(agg.FullLeverage-3.8) > 0.01 {
		t.Errorf("FullLeverage: want 3.8, got %f", agg.FullLeverage)
	}

	// SimpleLeverage = (5+3) / 1 = 8.0
	if math.Abs(agg.SimpleLeverage-8.0) > 0.01 {
		t.Errorf("SimpleLeverage: want 8.0, got %f", agg.SimpleLeverage)
	}

	if agg.TotalLines != 3000 {
		t.Errorf("TotalLines: want 3000, got %d", agg.TotalLines)
	}
	if agg.EstimatedCost != 125000 {
		t.Errorf("EstimatedCost: want 125000, got %f", agg.EstimatedCost)
	}
}

func TestAggregateScores_PerProjectActualPeople(t *testing.T) {
	// Two projects with different team sizes and durations
	scores := []*LeverageScore{
		{
			EstimatedPeople: 10.0,
			EstimatedMonths: 5.0,
			EstimatedCost:   100000,
			ActualPeople:    1, // solo project
			ElapsedMonths:   6.0,
			AuthorCount:     1,
			TotalCode:       5000,
		},
		{
			EstimatedPeople: 20.0,
			EstimatedMonths: 10.0,
			EstimatedCost:   500000,
			ActualPeople:    3, // 3-person project
			ElapsedMonths:   4.0,
			AuthorCount:     3,
			TotalCode:       20000,
		},
	}

	agg := AggregateScores(scores, 1, 10.0)

	// Total estimated PM = (10*5) + (20*10) = 50 + 200 = 250
	// Total actual PM = (1*6) + (3*4) = 6 + 12 = 18
	// FullLeverage = 250 / 18 ≈ 13.89
	wantLeverage := 250.0 / 18.0
	if math.Abs(agg.FullLeverage-wantLeverage) > 0.1 {
		t.Errorf("FullLeverage: want %.2f, got %.2f", wantLeverage, agg.FullLeverage)
	}

	if math.Abs(agg.ActualPersonMonths-18.0) > 0.01 {
		t.Errorf("ActualPersonMonths: want 18.0, got %.2f", agg.ActualPersonMonths)
	}

	if agg.AuthorCount != 3 {
		t.Errorf("AuthorCount: want 3 (max), got %d", agg.AuthorCount)
	}
}

func TestAggregateScores_FallbackToGlobalParams(t *testing.T) {
	// Scores without ActualPeople (legacy/snapshot scores)
	scores := []*LeverageScore{
		{
			EstimatedPeople: 10.0,
			EstimatedMonths: 5.0,
			// No ActualPeople or ElapsedMonths set (zero values)
			TotalCode: 5000,
		},
	}

	agg := AggregateScores(scores, 2, 8.0)

	// Should fall back to global: actual PM = 2 * 8 = 16
	// Estimated PM = 10 * 5 = 50
	// FullLeverage = 50 / 16 = 3.125
	if math.Abs(agg.FullLeverage-3.125) > 0.01 {
		t.Errorf("FullLeverage: want 3.125, got %.3f", agg.FullLeverage)
	}
}
