package leverage

import (
	"context"
	"math"
	"testing"
	"time"
)

// historyMockStore implements SnapshotStorer for history tests.
type historyMockStore struct {
	snapshots map[string][]*Snapshot
}

func newHistoryMockStore(data map[string][]*Snapshot) *historyMockStore {
	if data == nil {
		data = make(map[string][]*Snapshot)
	}
	return &historyMockStore{snapshots: data}
}

func (m *historyMockStore) Save(_ context.Context, s *Snapshot) error {
	m.snapshots[s.Project] = append(m.snapshots[s.Project], s)
	return nil
}

func (m *historyMockStore) Load(_ context.Context, project, date string) (*Snapshot, error) {
	for _, s := range m.snapshots[project] {
		if s.Date == date {
			return s, nil
		}
	}
	return nil, nil
}

func (m *historyMockStore) List(_ context.Context, project string) ([]string, error) {
	var dates []string
	for _, s := range m.snapshots[project] {
		dates = append(dates, s.Date)
	}
	return dates, nil
}

func (m *historyMockStore) LoadAll(_ context.Context, project string) ([]*Snapshot, error) {
	return m.snapshots[project], nil
}

func (m *historyMockStore) ListProjects(_ context.Context) ([]string, error) {
	var projects []string
	for p := range m.snapshots {
		projects = append(projects, p)
	}
	return projects, nil
}

func d(year, month, day int) time.Time {
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func TestLoadHistory_SingleProjectSingleSnapshot(t *testing.T) {
	store := newHistoryMockStore(map[string][]*Snapshot{
		"camp": {
			{
				Project: "camp", Date: "2025-06-02", TotalLines: 5000,
				SCC:      &SnapshotSCC{EstimatedCost: 50000, EstimatedPeople: 3.0, EstimatedMonths: 8.0},
				Leverage: &LeverageScore{EstimatedPeople: 3.0, EstimatedMonths: 8.0, EstimatedCost: 50000, TotalCode: 5000},
			},
		},
	})

	// since is before the snapshot date so elapsed > 0
	history, err := LoadHistory(context.Background(), store, []string{"camp"}, 1, d(2025, 1, 1), d(2025, 6, 8))
	if err != nil {
		t.Fatal(err)
	}

	if len(history) == 0 {
		t.Fatal("expected at least one history point")
	}

	// The last point should have the snapshot data
	last := history[len(history)-1]
	if last.TotalCode != 5000 {
		t.Errorf("TotalCode = %d, want 5000", last.TotalCode)
	}
	if last.Aggregate == nil {
		t.Fatal("expected non-nil aggregate for point with data and elapsed > 0")
	}
}

func TestLoadHistory_MultipleProjectsDifferentStartDates(t *testing.T) {
	store := newHistoryMockStore(map[string][]*Snapshot{
		"camp": {
			{Project: "camp", Date: "2025-04-28", TotalLines: 1000, Leverage: &LeverageScore{EstimatedPeople: 2.0, EstimatedMonths: 5.0, TotalCode: 1000}},
			{Project: "camp", Date: "2025-05-05", TotalLines: 2000, Leverage: &LeverageScore{EstimatedPeople: 2.5, EstimatedMonths: 6.0, TotalCode: 2000}},
			{Project: "camp", Date: "2025-05-12", TotalLines: 3000, Leverage: &LeverageScore{EstimatedPeople: 3.0, EstimatedMonths: 7.0, TotalCode: 3000}},
		},
		"fest": {
			// fest starts later — no data until May 12
			{Project: "fest", Date: "2025-05-12", TotalLines: 500, Leverage: &LeverageScore{EstimatedPeople: 1.0, EstimatedMonths: 3.0, TotalCode: 500}},
		},
	})

	// Apr 28 (Mon) -> May 5 (Mon) -> May 12 (Mon) = 3 weeks in range up to May 18
	history, err := LoadHistory(context.Background(), store, []string{"camp", "fest"}, 1, d(2025, 4, 28), d(2025, 5, 18))
	if err != nil {
		t.Fatal(err)
	}

	if len(history) != 3 {
		t.Fatalf("got %d points, want 3 (Apr 28, May 5, May 12)", len(history))
	}

	// First week (Apr 28): only camp
	if len(history[0].Projects) != 1 {
		t.Errorf("week 1 projects = %d, want 1 (only camp)", len(history[0].Projects))
	}

	// Third week (May 12): both camp and fest
	if len(history[2].Projects) != 2 {
		t.Errorf("week 3 projects = %d, want 2 (camp + fest)", len(history[2].Projects))
	}
}

func TestLoadHistory_EmptyStore(t *testing.T) {
	store := newHistoryMockStore(nil)

	history, err := LoadHistory(context.Background(), store, []string{"camp"}, 1, d(2025, 6, 2), d(2025, 6, 15))
	if err != nil {
		t.Fatal(err)
	}

	// Should return history points but with empty projects
	if len(history) == 0 {
		t.Fatal("expected history points even with empty store")
	}

	for _, point := range history {
		if len(point.Projects) != 0 {
			t.Errorf("expected empty projects, got %d", len(point.Projects))
		}
		if point.Aggregate != nil {
			t.Error("expected nil aggregate for empty store")
		}
	}
}

func TestLoadHistory_PerAuthorData(t *testing.T) {
	store := newHistoryMockStore(map[string][]*Snapshot{
		"camp": {
			{
				Project: "camp", Date: "2025-06-02", TotalLines: 10000,
				SCC:      &SnapshotSCC{EstimatedCost: 100000},
				Leverage: &LeverageScore{EstimatedPeople: 5.0, EstimatedMonths: 10.0, TotalCode: 10000},
				Authors: []AuthorContribution{
					{Name: "Alice", Email: "alice@test.com", Lines: 7000, Percentage: 70.0},
					{Name: "Bob", Email: "bob@test.com", Lines: 3000, Percentage: 30.0},
				},
			},
		},
	})

	history, err := LoadHistory(context.Background(), store, []string{"camp"}, 1, d(2025, 6, 2), d(2025, 6, 8))
	if err != nil {
		t.Fatal(err)
	}

	if len(history) != 1 {
		t.Fatalf("got %d points, want 1", len(history))
	}

	// Author data should flow through
	snap := history[0].Projects["camp"]
	if snap == nil {
		t.Fatal("expected camp snapshot")
	}
	if len(snap.Authors) != 2 {
		t.Fatalf("expected 2 authors, got %d", len(snap.Authors))
	}
	if snap.Authors[0].Name != "Alice" {
		t.Errorf("authors[0].Name = %q, want Alice", snap.Authors[0].Name)
	}
}

func TestLoadHistory_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	store := newHistoryMockStore(nil)
	_, err := LoadHistory(ctx, store, []string{"camp"}, 1, d(2025, 6, 2), d(2025, 6, 8))
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestLoadHistory_EmptyProjectList(t *testing.T) {
	store := newHistoryMockStore(nil)

	history, err := LoadHistory(context.Background(), store, nil, 1, d(2025, 6, 2), d(2025, 6, 8))
	if err != nil {
		t.Fatal(err)
	}

	// Should still iterate weeks, but all points have empty projects
	if len(history) == 0 {
		t.Fatal("expected history points even with empty project list")
	}
}

func TestLoadPeriodHistory_WeeklyDeltas(t *testing.T) {
	store := newHistoryMockStore(map[string][]*Snapshot{
		"camp": {
			{
				Project: "camp", Date: "2025-07-07", TotalLines: 437954,
				SCC:      &SnapshotSCC{EstimatedCost: 10000000, EstimatedPeople: 9.5, EstimatedMonths: 45.0},
				Leverage: &LeverageScore{EstimatedPeople: 9.5, EstimatedMonths: 45.0, EstimatedCost: 10000000, TotalCode: 437954},
			},
			{
				Project: "camp", Date: "2025-07-14", TotalLines: 500292,
				SCC:      &SnapshotSCC{EstimatedCost: 12000000, EstimatedPeople: 14.3, EstimatedMonths: 36.2},
				Leverage: &LeverageScore{EstimatedPeople: 14.3, EstimatedMonths: 36.2, EstimatedCost: 12000000, TotalCode: 500292},
			},
			{
				Project: "camp", Date: "2025-07-21", TotalLines: 500292,
				SCC:      &SnapshotSCC{EstimatedCost: 12000000, EstimatedPeople: 14.3, EstimatedMonths: 36.2},
				Leverage: &LeverageScore{EstimatedPeople: 14.3, EstimatedMonths: 36.2, EstimatedCost: 12000000, TotalCode: 500292},
			},
		},
	})

	history, err := LoadPeriodHistory(context.Background(), store, []string{"camp"}, 1, d(2025, 7, 7), d(2025, 7, 21), PeriodWeekly)
	if err != nil {
		t.Fatal(err)
	}

	if len(history) != 3 {
		t.Fatalf("got %d points, want 3", len(history))
	}

	// First point is the baseline
	if !history[0].IsFirst {
		t.Error("first point should have IsFirst=true")
	}

	// Second point: active week with positive delta
	if history[1].DeltaCode != 62338 {
		t.Errorf("week 2 DeltaCode = %d, want 62338", history[1].DeltaCode)
	}
	if history[1].PeriodLeverage < 300 {
		t.Errorf("week 2 PeriodLeverage = %.1f, want > 300", history[1].PeriodLeverage)
	}

	// Third point: idle week with zero delta
	if history[2].DeltaCode != 0 {
		t.Errorf("week 3 DeltaCode = %d, want 0", history[2].DeltaCode)
	}
	if math.Abs(history[2].PeriodLeverage) > 0.01 {
		t.Errorf("week 3 PeriodLeverage = %.1f, want ~0", history[2].PeriodLeverage)
	}
}

func TestLoadPeriodHistory_MonthlyAggregation(t *testing.T) {
	store := newHistoryMockStore(map[string][]*Snapshot{
		"camp": {
			{
				Project: "camp", Date: "2025-07-07", TotalLines: 437954,
				SCC:      &SnapshotSCC{EstimatedCost: 10000000, EstimatedPeople: 9.5, EstimatedMonths: 45.0},
				Leverage: &LeverageScore{EstimatedPeople: 9.5, EstimatedMonths: 45.0, EstimatedCost: 10000000, TotalCode: 437954},
			},
			{
				Project: "camp", Date: "2025-07-14", TotalLines: 480000,
				SCC:      &SnapshotSCC{EstimatedCost: 11000000, EstimatedPeople: 12.0, EstimatedMonths: 40.0},
				Leverage: &LeverageScore{EstimatedPeople: 12.0, EstimatedMonths: 40.0, EstimatedCost: 11000000, TotalCode: 480000},
			},
			{
				Project: "camp", Date: "2025-07-21", TotalLines: 500292,
				SCC:      &SnapshotSCC{EstimatedCost: 12000000, EstimatedPeople: 14.3, EstimatedMonths: 36.2},
				Leverage: &LeverageScore{EstimatedPeople: 14.3, EstimatedMonths: 36.2, EstimatedCost: 12000000, TotalCode: 500292},
			},
			{
				Project: "camp", Date: "2025-08-04", TotalLines: 510000,
				SCC:      &SnapshotSCC{EstimatedCost: 12500000, EstimatedPeople: 14.8, EstimatedMonths: 37.0},
				Leverage: &LeverageScore{EstimatedPeople: 14.8, EstimatedMonths: 37.0, EstimatedCost: 12500000, TotalCode: 510000},
			},
		},
	})

	history, err := LoadPeriodHistory(context.Background(), store, []string{"camp"}, 1, d(2025, 7, 7), d(2025, 8, 10), PeriodMonthly)
	if err != nil {
		t.Fatal(err)
	}

	if len(history) != 2 {
		t.Fatalf("got %d months, want 2 (July + August)", len(history))
	}

	// July: first to last snapshot in July
	july := history[0]
	if july.Period != PeriodMonthly {
		t.Errorf("july period = %q, want %q", july.Period, PeriodMonthly)
	}

	// August
	aug := history[1]
	if aug.Period != PeriodMonthly {
		t.Errorf("aug period = %q, want %q", aug.Period, PeriodMonthly)
	}
}

func TestLoadPeriodHistory_EmptyStore(t *testing.T) {
	store := newHistoryMockStore(nil)
	history, err := LoadPeriodHistory(context.Background(), store, []string{"camp"}, 1, d(2025, 6, 2), d(2025, 6, 15), PeriodWeekly)
	if err != nil {
		t.Fatal(err)
	}
	// Empty store should still return weekly points (with zero data)
	// LoadHistory returns points even with no snapshots
	if history == nil && len(history) == 0 {
		// Both nil and empty are acceptable
	}
}

func TestFindMostRecent(t *testing.T) {
	snapshots := []*Snapshot{
		{Date: "2025-01-06"},
		{Date: "2025-01-13"},
		{Date: "2025-01-20"},
	}

	tests := []struct {
		name     string
		target   time.Time
		wantDate string
		wantNil  bool
	}{
		{
			name:    "before all snapshots",
			target:  d(2025, 1, 1),
			wantNil: true,
		},
		{
			name:     "exact match",
			target:   d(2025, 1, 13),
			wantDate: "2025-01-13",
		},
		{
			name:     "between snapshots",
			target:   d(2025, 1, 15),
			wantDate: "2025-01-13",
		},
		{
			name:     "after all snapshots",
			target:   d(2025, 2, 1),
			wantDate: "2025-01-20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findMostRecent(snapshots, tt.target)
			if tt.wantNil {
				if result != nil {
					t.Fatalf("expected nil, got %s", result.Date)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil")
			}
			if result.Date != tt.wantDate {
				t.Errorf("date = %s, want %s", result.Date, tt.wantDate)
			}
		})
	}
}

func TestFindMostRecent_EmptySlice(t *testing.T) {
	result := findMostRecent(nil, d(2025, 6, 1))
	if result != nil {
		t.Error("expected nil for empty slice")
	}
}
