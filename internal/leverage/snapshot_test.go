package leverage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testSnapshot(project string, commitDate time.Time) *Snapshot {
	return &Snapshot{
		Project:    project,
		CommitHash: "abc123def456",
		CommitDate: commitDate,
		SampledAt:  time.Now().Truncate(time.Second),
		SCC: &SnapshotSCC{
			EstimatedCost:   697323.22,
			EstimatedMonths: 11.99,
			EstimatedPeople: 5.17,
			TotalFiles:      133,
			TotalLines:      27807,
			TotalCode:       21397,
			TotalComments:   2370,
			TotalBlanks:     4040,
			TotalComplexity: 4668,
			Languages: []LanguageSummary{
				{Name: "Go", Files: 133, Code: 21397, Complexity: 4668},
			},
		},
		Leverage: &LeverageScore{
			ProjectName:    project,
			EstimatedPeople: 5.17,
			EstimatedMonths: 11.99,
			EstimatedCost:   697323.22,
			ActualPeople:    1,
			ElapsedMonths:   9.4,
			FullLeverage:    265.9,
			SimpleLeverage:  5.17,
			TotalLines:      27807,
			TotalCode:       21397,
		},
		Authors: []AuthorContribution{
			{Name: "Alice", Email: "alice@example.com", Lines: 15000, Percentage: 70.12},
			{Name: "Bob", Email: "bob@example.com", Lines: 6397, Percentage: 29.88},
		},
		TotalLines: 27807,
	}
}

func TestFileSnapshotStore_SaveAndLoad(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileSnapshotStore(baseDir)

	commitDate := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	snap := testSnapshot("camp", commitDate)

	if err := store.Save(context.Background(), snap); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists
	path := filepath.Join(baseDir, "camp", "2025-06-15.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot file not created: %v", err)
	}

	// Load and verify round-trip
	loaded, err := store.Load(context.Background(), "camp", "2025-06-15")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Project != snap.Project {
		t.Errorf("Project = %q, want %q", loaded.Project, snap.Project)
	}
	if loaded.CommitHash != snap.CommitHash {
		t.Errorf("CommitHash = %q, want %q", loaded.CommitHash, snap.CommitHash)
	}
	if loaded.Date != "2025-06-15" {
		t.Errorf("Date = %q, want %q", loaded.Date, "2025-06-15")
	}

	// Verify SCC fields
	if loaded.SCC.TotalCode != snap.SCC.TotalCode {
		t.Errorf("SCC.TotalCode = %d, want %d", loaded.SCC.TotalCode, snap.SCC.TotalCode)
	}
	if loaded.SCC.EstimatedCost != snap.SCC.EstimatedCost {
		t.Errorf("SCC.EstimatedCost = %f, want %f", loaded.SCC.EstimatedCost, snap.SCC.EstimatedCost)
	}

	// Verify authors
	if len(loaded.Authors) != 2 {
		t.Fatalf("Authors count = %d, want 2", len(loaded.Authors))
	}
	if loaded.Authors[0].Name != "Alice" {
		t.Errorf("Authors[0].Name = %q, want %q", loaded.Authors[0].Name, "Alice")
	}
	if loaded.Authors[0].Lines != 15000 {
		t.Errorf("Authors[0].Lines = %d, want %d", loaded.Authors[0].Lines, 15000)
	}

	// Verify leverage
	if loaded.Leverage.FullLeverage != snap.Leverage.FullLeverage {
		t.Errorf("Leverage.FullLeverage = %f, want %f", loaded.Leverage.FullLeverage, snap.Leverage.FullLeverage)
	}
}

func TestFileSnapshotStore_SaveOverwrite(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileSnapshotStore(baseDir)
	ctx := context.Background()

	commitDate := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)

	snap1 := testSnapshot("camp", commitDate)
	snap1.TotalLines = 1000

	snap2 := testSnapshot("camp", commitDate)
	snap2.TotalLines = 2000

	if err := store.Save(ctx, snap1); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(ctx, snap2); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load(ctx, "camp", "2025-06-15")
	if err != nil {
		t.Fatal(err)
	}

	if loaded.TotalLines != 2000 {
		t.Errorf("TotalLines = %d, want 2000 (second save should overwrite)", loaded.TotalLines)
	}
}

func TestFileSnapshotStore_ListSorted(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileSnapshotStore(baseDir)
	ctx := context.Background()

	// Save snapshots out of order
	dates := []time.Time{
		time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 12, 25, 0, 0, 0, 0, time.UTC),
	}
	for _, d := range dates {
		if err := store.Save(ctx, testSnapshot("camp", d)); err != nil {
			t.Fatal(err)
		}
	}

	list, err := store.List(ctx, "camp")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"2025-03-01", "2025-06-15", "2025-12-25"}
	if len(list) != len(want) {
		t.Fatalf("List length = %d, want %d", len(list), len(want))
	}
	for i, d := range want {
		if list[i] != d {
			t.Errorf("List[%d] = %q, want %q", i, list[i], d)
		}
	}
}

func TestFileSnapshotStore_ListEmptyProject(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileSnapshotStore(baseDir)

	list, err := store.List(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %v", list)
	}
}

func TestFileSnapshotStore_LoadAll(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileSnapshotStore(baseDir)
	ctx := context.Background()

	dates := []time.Time{
		time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 6, 15, 0, 0, 0, 0, time.UTC),
	}
	for _, d := range dates {
		if err := store.Save(ctx, testSnapshot("fest", d)); err != nil {
			t.Fatal(err)
		}
	}

	all, err := store.LoadAll(ctx, "fest")
	if err != nil {
		t.Fatal(err)
	}

	if len(all) != 2 {
		t.Fatalf("LoadAll returned %d, want 2", len(all))
	}
	if all[0].Date != "2025-03-01" {
		t.Errorf("first snapshot date = %q, want 2025-03-01", all[0].Date)
	}
	if all[1].Date != "2025-06-15" {
		t.Errorf("second snapshot date = %q, want 2025-06-15", all[1].Date)
	}
}

func TestFileSnapshotStore_ListProjects(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileSnapshotStore(baseDir)
	ctx := context.Background()

	for _, proj := range []string{"fest", "camp", "obey"} {
		d := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		if err := store.Save(ctx, testSnapshot(proj, d)); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := store.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"camp", "fest", "obey"}
	if len(projects) != len(want) {
		t.Fatalf("ListProjects = %v, want %v", projects, want)
	}
	for i, p := range want {
		if projects[i] != p {
			t.Errorf("projects[%d] = %q, want %q", i, projects[i], p)
		}
	}
}

func TestFileSnapshotStore_ListProjectsEmpty(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileSnapshotStore(baseDir)

	// baseDir exists but is empty
	projects, err := store.ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Errorf("expected empty, got %v", projects)
	}
}

func TestFileSnapshotStore_LoadNotFound(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileSnapshotStore(baseDir)

	_, err := store.Load(context.Background(), "nonexistent", "2025-01-01")
	if err == nil {
		t.Fatal("expected error for nonexistent snapshot")
	}
}

func TestFileSnapshotStore_SaveCreatesDirectories(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "deep", "nested")
	store := NewFileSnapshotStore(baseDir)

	d := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := store.Save(context.Background(), testSnapshot("camp", d)); err != nil {
		t.Fatalf("Save should create directories: %v", err)
	}

	path := filepath.Join(baseDir, "camp", "2025-01-01.json")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should exist: %v", err)
	}
}

func TestFileSnapshotStore_ContextCancellation(t *testing.T) {
	baseDir := t.TempDir()
	store := NewFileSnapshotStore(baseDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := store.Save(ctx, testSnapshot("camp", d)); err == nil {
		t.Fatal("expected context error")
	}

	if _, err := store.Load(ctx, "camp", "2025-01-01"); err == nil {
		t.Fatal("expected context error")
	}

	if _, err := store.List(ctx, "camp"); err == nil {
		t.Fatal("expected context error")
	}

	if _, err := store.ListProjects(ctx); err == nil {
		t.Fatal("expected context error")
	}

	// LoadAll with cancelled context
	if _, err := store.LoadAll(ctx, "camp"); err == nil {
		t.Fatal("expected context error from LoadAll")
	}
}

func TestDefaultSnapshotDir(t *testing.T) {
	got := DefaultSnapshotDir("/home/user/campaign")
	want := filepath.Join("/home/user/campaign", ".campaign", "leverage", "snapshots")
	if got != want {
		t.Errorf("DefaultSnapshotDir = %q, want %q", got, want)
	}
}
