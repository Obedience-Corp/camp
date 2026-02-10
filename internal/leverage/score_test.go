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
	// Actual person-months = 1 * 10 = 10
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
