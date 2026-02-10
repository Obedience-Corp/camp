package leverage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// mockSnapshotStore implements SnapshotStorer for testing.
type mockSnapshotStore struct {
	saved    []*Snapshot
	existing map[string][]string // project -> dates
}

func newMockSnapshotStore() *mockSnapshotStore {
	return &mockSnapshotStore{
		existing: make(map[string][]string),
	}
}

func (m *mockSnapshotStore) Save(_ context.Context, s *Snapshot) error {
	m.saved = append(m.saved, s)
	return nil
}

func (m *mockSnapshotStore) Load(_ context.Context, project, date string) (*Snapshot, error) {
	return nil, nil
}

func (m *mockSnapshotStore) List(_ context.Context, project string) ([]string, error) {
	return m.existing[project], nil
}

func (m *mockSnapshotStore) LoadAll(_ context.Context, project string) ([]*Snapshot, error) {
	return nil, nil
}

func (m *mockSnapshotStore) ListProjects(_ context.Context) ([]string, error) {
	return nil, nil
}

func TestGroupByGitDir_SingleProject(t *testing.T) {
	projects := []ResolvedProject{
		{Name: "camp", GitDir: "/repo/camp", SCCDir: "/repo/camp"},
	}

	groups := groupByGitDir(projects)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	if len(groups[0].Projects) != 1 {
		t.Errorf("group[0] has %d projects, want 1", len(groups[0].Projects))
	}
}

func TestGroupByGitDir_MonorepoGrouping(t *testing.T) {
	projects := []ResolvedProject{
		{Name: "obey", GitDir: "/monorepo", SCCDir: "/monorepo/obey", InMonorepo: true},
		{Name: "festui", GitDir: "/monorepo", SCCDir: "/monorepo/festui", InMonorepo: true},
		{Name: "camp", GitDir: "/repo/camp", SCCDir: "/repo/camp"},
	}

	groups := groupByGitDir(projects)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}

	// First group should be the monorepo (insertion order preserved)
	if groups[0].GitDir != "/monorepo" {
		t.Errorf("group[0].GitDir = %q, want /monorepo", groups[0].GitDir)
	}
	if len(groups[0].Projects) != 2 {
		t.Errorf("monorepo group has %d projects, want 2", len(groups[0].Projects))
	}

	// Second group is standalone camp
	if groups[1].GitDir != "/repo/camp" {
		t.Errorf("group[1].GitDir = %q, want /repo/camp", groups[1].GitDir)
	}
	if len(groups[1].Projects) != 1 {
		t.Errorf("camp group has %d projects, want 1", len(groups[1].Projects))
	}
}

func TestGroupByGitDir_PreservesOrder(t *testing.T) {
	projects := []ResolvedProject{
		{Name: "a", GitDir: "/dir-a", SCCDir: "/dir-a"},
		{Name: "b", GitDir: "/dir-b", SCCDir: "/dir-b"},
		{Name: "c", GitDir: "/dir-c", SCCDir: "/dir-c"},
	}

	groups := groupByGitDir(projects)
	if len(groups) != 3 {
		t.Fatalf("got %d groups, want 3", len(groups))
	}
	for i, want := range []string{"a", "b", "c"} {
		if groups[i].Projects[0].Name != want {
			t.Errorf("groups[%d].Projects[0].Name = %q, want %q", i, groups[i].Projects[0].Name, want)
		}
	}
}

func TestContainsDate(t *testing.T) {
	dates := []string{"2025-01-01", "2025-06-15", "2025-12-25"}

	if !containsDate(dates, "2025-06-15") {
		t.Error("expected containsDate to find 2025-06-15")
	}
	if containsDate(dates, "2025-07-04") {
		t.Error("expected containsDate to not find 2025-07-04")
	}
	if containsDate(nil, "2025-01-01") {
		t.Error("expected containsDate on nil to return false")
	}
}

func TestNewBackfiller_MinWorkers(t *testing.T) {
	store := newMockSnapshotStore()
	b := NewBackfiller(&MockRunner{}, store, 0)
	if b.workers != 1 {
		t.Errorf("workers = %d, want 1 (minimum)", b.workers)
	}
}

func TestBackfiller_EmptyProjects(t *testing.T) {
	store := newMockSnapshotStore()
	b := NewBackfiller(&MockRunner{}, store, 2)

	err := b.Run(context.Background(), nil, &LeverageConfig{
		ProjectStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("expected no error for empty projects, got: %v", err)
	}
	if len(store.saved) != 0 {
		t.Errorf("expected no saved snapshots, got %d", len(store.saved))
	}
}

func TestBackfiller_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	store := newMockSnapshotStore()
	b := NewBackfiller(&MockRunner{}, store, 1)

	err := b.Run(ctx, []ResolvedProject{
		{Name: "test", GitDir: t.TempDir(), SCCDir: t.TempDir()},
	}, &LeverageConfig{
		ProjectStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	})

	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestBackfiller_ProgressCallback(t *testing.T) {
	var callCount atomic.Int32

	store := newMockSnapshotStore()
	b := NewBackfiller(&MockRunner{}, store, 1)
	b.SetProgressCallback(func(project string, current, total int) {
		callCount.Add(1)
	})

	// With no projects, callback should not fire
	err := b.Run(context.Background(), nil, &LeverageConfig{
		ProjectStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if callCount.Load() != 0 {
		t.Errorf("callback called %d times, want 0 for empty projects", callCount.Load())
	}
}

func TestSCCResultToSnapshotSCC(t *testing.T) {
	result := &SCCResult{
		LanguageSummary: []LanguageEntry{
			{Name: "Go", Lines: 1000, Code: 800, Comment: 100, Blank: 100, Count: 10, Complexity: 50},
			{Name: "Python", Lines: 500, Code: 400, Comment: 50, Blank: 50, Count: 5, Complexity: 20},
		},
		EstimatedCost:           100000.0,
		EstimatedScheduleMonths: 12.0,
		EstimatedPeople:         5.0,
	}

	scc := SCCResultToSnapshotSCC(result)

	if scc.TotalFiles != 15 {
		t.Errorf("TotalFiles = %d, want 15", scc.TotalFiles)
	}
	if scc.TotalLines != 1500 {
		t.Errorf("TotalLines = %d, want 1500", scc.TotalLines)
	}
	if scc.TotalCode != 1200 {
		t.Errorf("TotalCode = %d, want 1200", scc.TotalCode)
	}
	if scc.TotalComments != 150 {
		t.Errorf("TotalComments = %d, want 150", scc.TotalComments)
	}
	if scc.TotalBlanks != 150 {
		t.Errorf("TotalBlanks = %d, want 150", scc.TotalBlanks)
	}
	if scc.TotalComplexity != 70 {
		t.Errorf("TotalComplexity = %d, want 70", scc.TotalComplexity)
	}
	if len(scc.Languages) != 2 {
		t.Fatalf("Languages count = %d, want 2", len(scc.Languages))
	}
	if scc.Languages[0].Name != "Go" {
		t.Errorf("Languages[0].Name = %q, want Go", scc.Languages[0].Name)
	}
	if scc.EstimatedCost != 100000.0 {
		t.Errorf("EstimatedCost = %f, want 100000.0", scc.EstimatedCost)
	}
}

func TestCreateWorktree(t *testing.T) {
	// Create a real git repo with at least one commit
	dir := initGitRepoWithCommits(t, []time.Time{
		time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	})

	// Get the HEAD commit hash
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	hash := string(out[:len(out)-1]) // trim newline

	worktreeDir, cleanup, err := createWorktree(context.Background(), dir, hash)
	if err != nil {
		t.Fatalf("createWorktree: %v", err)
	}

	// Verify worktree exists and has content
	if _, err := os.Stat(worktreeDir); err != nil {
		t.Fatalf("worktree dir should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(worktreeDir, "file.txt")); err != nil {
		t.Fatalf("worktree should contain file.txt: %v", err)
	}

	// Cleanup and verify worktree is removed
	cleanup()
	if _, err := os.Stat(worktreeDir); !os.IsNotExist(err) {
		t.Errorf("worktree dir should be removed after cleanup")
	}
}

func TestBackfiller_Integration(t *testing.T) {
	// Skip if scc is not available
	if _, err := exec.LookPath("scc"); err != nil {
		t.Skip("scc not installed, skipping integration test")
	}

	// Create a git repo with commits across 3 different weeks
	dates := []time.Time{
		time.Date(2025, 6, 2, 10, 0, 0, 0, time.UTC),  // Week 23
		time.Date(2025, 6, 9, 10, 0, 0, 0, time.UTC),  // Week 24
		time.Date(2025, 6, 16, 10, 0, 0, 0, time.UTC), // Week 25
	}
	dir := initGitRepoWithCommits(t, dates)

	runner, err := NewSCCRunner(COCOMOOrganic)
	if err != nil {
		t.Fatal(err)
	}

	store := newMockSnapshotStore()
	b := NewBackfiller(runner, store, 1)

	var progressCalls atomic.Int32
	b.SetProgressCallback(func(project string, current, total int) {
		progressCalls.Add(1)
	})

	cfg := &LeverageConfig{
		ActualPeople: 1,
		ProjectStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	projects := []ResolvedProject{
		{Name: "test-project", GitDir: dir, SCCDir: dir},
	}

	if err := b.Run(context.Background(), projects, cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have created snapshots (3 weekly commits = 3 samples)
	if len(store.saved) == 0 {
		t.Fatal("expected at least one saved snapshot")
	}
	if len(store.saved) > 4 {
		t.Errorf("expected <= 4 snapshots (3 weeks + possible first/last), got %d", len(store.saved))
	}

	// Verify snapshot structure
	for _, snap := range store.saved {
		if snap.Project != "test-project" {
			t.Errorf("snapshot project = %q, want test-project", snap.Project)
		}
		if snap.CommitHash == "" {
			t.Error("snapshot commit hash should not be empty")
		}
		if snap.SCC == nil {
			t.Error("snapshot SCC should not be nil")
		}
		if snap.Leverage == nil {
			t.Error("snapshot Leverage should not be nil")
		}
	}

	// Progress should have been called
	if progressCalls.Load() == 0 {
		t.Error("expected progress callback to be called")
	}
}

func TestBackfiller_Incremental(t *testing.T) {
	// Skip if scc is not available
	if _, err := exec.LookPath("scc"); err != nil {
		t.Skip("scc not installed, skipping integration test")
	}

	dates := []time.Time{
		time.Date(2025, 6, 2, 10, 0, 0, 0, time.UTC),
		time.Date(2025, 6, 9, 10, 0, 0, 0, time.UTC),
	}
	dir := initGitRepoWithCommits(t, dates)

	runner, err := NewSCCRunner(COCOMOOrganic)
	if err != nil {
		t.Fatal(err)
	}

	// Pre-populate store with one date as "already exists"
	store := newMockSnapshotStore()
	store.existing["test-project"] = []string{"2025-06-02"}

	b := NewBackfiller(runner, store, 1)

	cfg := &LeverageConfig{
		ActualPeople: 1,
		ProjectStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	projects := []ResolvedProject{
		{Name: "test-project", GitDir: dir, SCCDir: dir},
	}

	if err := b.Run(context.Background(), projects, cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have fewer snapshots than total weeks because 2025-06-02 was skipped
	for _, snap := range store.saved {
		if snap.Date == "2025-06-02" {
			t.Error("incremental backfill should NOT have created snapshot for existing date 2025-06-02")
		}
	}
}

func TestBackfiller_MonorepoMissingSubdir(t *testing.T) {
	// Skip if scc is not available
	if _, err := exec.LookPath("scc"); err != nil {
		t.Skip("scc not installed, skipping integration test")
	}

	// Create a monorepo-like setup: one commit with a Go file in subdir "pkg"
	dir := t.TempDir()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.name", "Test"},
		{"git", "config", "user.email", "test@example.com"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s: %v", args, out, err)
		}
	}

	// Create subdir "pkg" with a Go file
	pkg := filepath.Join(dir, "pkg")
	os.MkdirAll(pkg, 0o755)
	os.WriteFile(filepath.Join(pkg, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)

	addCommit := exec.Command("git", "add", ".")
	addCommit.Dir = dir
	addCommit.Run()

	env := []string{
		"GIT_COMMITTER_DATE=2025-06-15T10:00:00Z",
		"GIT_AUTHOR_DATE=2025-06-15T10:00:00Z",
	}
	commitCmd := exec.Command("git", "commit", "-m", "init")
	commitCmd.Dir = dir
	commitCmd.Env = append(os.Environ(), env...)
	commitCmd.Run()

	runner, err := NewSCCRunner(COCOMOOrganic)
	if err != nil {
		t.Fatal(err)
	}

	store := newMockSnapshotStore()
	b := NewBackfiller(runner, store, 1)

	cfg := &LeverageConfig{
		ActualPeople: 1,
		ProjectStart: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	// Two "projects" in the same git dir: pkg (exists) and missing (doesn't exist)
	projects := []ResolvedProject{
		{Name: "pkg-project", GitDir: dir, SCCDir: filepath.Join(dir, "pkg"), InMonorepo: true},
		{Name: "missing-project", GitDir: dir, SCCDir: filepath.Join(dir, "nonexistent"), InMonorepo: true},
	}

	if err := b.Run(context.Background(), projects, cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// pkg-project should have snapshots, missing-project should have none
	var pkgCount, missingCount int
	for _, snap := range store.saved {
		switch snap.Project {
		case "pkg-project":
			pkgCount++
		case "missing-project":
			missingCount++
		}
	}

	if pkgCount == 0 {
		t.Error("expected at least one snapshot for pkg-project")
	}
	if missingCount != 0 {
		t.Errorf("expected zero snapshots for missing-project, got %d", missingCount)
	}
}
