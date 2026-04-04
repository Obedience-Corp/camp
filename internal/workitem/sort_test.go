package workitem

import (
	"testing"
	"time"
)

func TestSort_DeterministicOrder(t *testing.T) {
	now := time.Now()
	items := []WorkItem{
		{RelativePath: "b", SortTimestamp: now.Add(-1 * time.Hour), CreatedAt: now.Add(-2 * time.Hour)},
		{RelativePath: "a", SortTimestamp: now, CreatedAt: now},
		{RelativePath: "c", SortTimestamp: now, CreatedAt: now},
	}
	Sort(items)

	if items[0].RelativePath != "a" {
		t.Errorf("items[0] = %q, want 'a' (most recent sort_timestamp)", items[0].RelativePath)
	}
	if items[1].RelativePath != "c" {
		t.Errorf("items[1] = %q, want 'c' (same timestamp, alphabetical tiebreak)", items[1].RelativePath)
	}
	if items[2].RelativePath != "b" {
		t.Errorf("items[2] = %q, want 'b' (oldest)", items[2].RelativePath)
	}
}

func TestSort_CreatedAtTiebreak(t *testing.T) {
	now := time.Now()
	items := []WorkItem{
		{RelativePath: "old", SortTimestamp: now, CreatedAt: now.Add(-1 * time.Hour)},
		{RelativePath: "new", SortTimestamp: now, CreatedAt: now},
	}
	Sort(items)

	if items[0].RelativePath != "new" {
		t.Errorf("items[0] = %q, want 'new' (newer created_at breaks sort_timestamp tie)", items[0].RelativePath)
	}
}

func TestSort_EmptySlice(t *testing.T) {
	Sort(nil)
	Sort([]WorkItem{})
}

func TestSort_SingleItem(t *testing.T) {
	items := []WorkItem{{RelativePath: "only"}}
	Sort(items)
	if items[0].RelativePath != "only" {
		t.Error("single item sort failed")
	}
}

func TestDeriveSortTimestamp_PrefersUpdated(t *testing.T) {
	now := time.Now()
	created := now.Add(-1 * time.Hour)

	ts := DeriveSortTimestamp(now, created)
	if !ts.Equal(now) {
		t.Errorf("expected updated_at when non-zero, got %v", ts)
	}
}

func TestDeriveSortTimestamp_FallsBackToCreated(t *testing.T) {
	created := time.Now()
	ts := DeriveSortTimestamp(time.Time{}, created)
	if !ts.Equal(created) {
		t.Errorf("expected created_at when updated_at is zero, got %v", ts)
	}
}

func TestSort_ManualPriorityBuckets(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		items []WorkItem
		want  []string // expected RelativePath order
	}{
		{
			name: "high before medium",
			items: []WorkItem{
				{RelativePath: "med", ManualPriority: "medium", SortTimestamp: now},
				{RelativePath: "hi", ManualPriority: "high", SortTimestamp: now},
			},
			want: []string{"hi", "med"},
		},
		{
			name: "medium before low",
			items: []WorkItem{
				{RelativePath: "lo", ManualPriority: "low", SortTimestamp: now},
				{RelativePath: "med", ManualPriority: "medium", SortTimestamp: now},
			},
			want: []string{"med", "lo"},
		},
		{
			name: "low before unset",
			items: []WorkItem{
				{RelativePath: "unset", SortTimestamp: now},
				{RelativePath: "lo", ManualPriority: "low", SortTimestamp: now},
			},
			want: []string{"lo", "unset"},
		},
		{
			name: "full bucket order",
			items: []WorkItem{
				{RelativePath: "unset", SortTimestamp: now},
				{RelativePath: "lo", ManualPriority: "low", SortTimestamp: now},
				{RelativePath: "hi", ManualPriority: "high", SortTimestamp: now},
				{RelativePath: "med", ManualPriority: "medium", SortTimestamp: now},
			},
			want: []string{"hi", "med", "lo", "unset"},
		},
		{
			name: "same priority uses recency",
			items: []WorkItem{
				{RelativePath: "old", ManualPriority: "high", SortTimestamp: now.Add(-time.Hour)},
				{RelativePath: "new", ManualPriority: "high", SortTimestamp: now},
			},
			want: []string{"new", "old"},
		},
		{
			name: "same priority same timestamp uses path",
			items: []WorkItem{
				{RelativePath: "b", ManualPriority: "medium", SortTimestamp: now, CreatedAt: now},
				{RelativePath: "a", ManualPriority: "medium", SortTimestamp: now, CreatedAt: now},
			},
			want: []string{"a", "b"},
		},
		{
			name: "all unset backward compat",
			items: []WorkItem{
				{RelativePath: "c", SortTimestamp: now.Add(-2 * time.Hour)},
				{RelativePath: "a", SortTimestamp: now},
				{RelativePath: "b", SortTimestamp: now.Add(-time.Hour)},
			},
			want: []string{"a", "b", "c"},
		},
		{
			name: "priority wins over recency",
			items: []WorkItem{
				{RelativePath: "new-unset", SortTimestamp: now},
				{RelativePath: "old-high", ManualPriority: "high", SortTimestamp: now.Add(-time.Hour)},
			},
			want: []string{"old-high", "new-unset"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Sort(tt.items)
			for i, wantPath := range tt.want {
				if tt.items[i].RelativePath != wantPath {
					t.Errorf("items[%d].RelativePath = %q, want %q", i, tt.items[i].RelativePath, wantPath)
				}
			}
		})
	}
}
