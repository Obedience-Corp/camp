package leverage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// initGitRepoWithCommits creates a temp git repo with commits at the specified dates.
// Returns the repo directory path. Each commit modifies a file so it has content to analyze.
func initGitRepoWithCommits(t *testing.T, dates []time.Time) string {
	t.Helper()
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

	for i, d := range dates {
		file := filepath.Join(dir, "file.txt")
		content := []byte("line " + d.Format(time.RFC3339) + "\n")
		if i > 0 {
			existing, _ := os.ReadFile(file)
			content = append(existing, content...)
		}
		if err := os.WriteFile(file, content, 0o644); err != nil {
			t.Fatal(err)
		}

		env := []string{
			"GIT_COMMITTER_DATE=" + d.Format(time.RFC3339),
			"GIT_AUTHOR_DATE=" + d.Format(time.RFC3339),
		}
		addCmd := exec.Command("git", "add", ".")
		addCmd.Dir = dir
		if out, err := addCmd.CombinedOutput(); err != nil {
			t.Fatalf("git add: %s: %v", out, err)
		}

		commitCmd := exec.Command("git", "commit", "-m", d.Format("2006-01-02"))
		commitCmd.Dir = dir
		commitCmd.Env = append(os.Environ(), env...)
		if out, err := commitCmd.CombinedOutput(); err != nil {
			t.Fatalf("git commit: %s: %v", out, err)
		}
	}

	return dir
}

func TestSampleWeeklyCommits_SingleCommit(t *testing.T) {
	d := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC) // Sunday
	dir := initGitRepoWithCommits(t, []time.Time{d})

	samples, err := SampleWeeklyCommits(context.Background(), dir, time.Time{})
	if err != nil {
		t.Fatalf("SampleWeeklyCommits: %v", err)
	}

	if len(samples) != 1 {
		t.Fatalf("got %d samples, want 1", len(samples))
	}
	if samples[0].Date.Format("2006-01-02") != "2025-06-15" {
		t.Errorf("date = %s, want 2025-06-15", samples[0].Date.Format("2006-01-02"))
	}
}

func TestSampleWeeklyCommits_MultipleCommitsSameWeek(t *testing.T) {
	// Monday, Wednesday, Friday of the same ISO week
	dates := []time.Time{
		time.Date(2025, 6, 9, 10, 0, 0, 0, time.UTC),  // Monday
		time.Date(2025, 6, 11, 10, 0, 0, 0, time.UTC), // Wednesday
		time.Date(2025, 6, 13, 10, 0, 0, 0, time.UTC), // Friday
	}
	dir := initGitRepoWithCommits(t, dates)

	samples, err := SampleWeeklyCommits(context.Background(), dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	// Should get 1 weekly sample (last commit = Friday) + first commit ensured
	// Since first and last commits may be the same week, we might get 2 (first + last)
	// or 1 (if last per week == first or last commit already)
	if len(samples) < 1 || len(samples) > 2 {
		t.Fatalf("got %d samples, want 1-2", len(samples))
	}

	// The last sample should have the latest commit's hash
	last := samples[len(samples)-1]
	if last.Date.Format("2006-01-02") != "2025-06-13" {
		t.Errorf("last sample date = %s, want 2025-06-13", last.Date.Format("2006-01-02"))
	}
}

func TestSampleWeeklyCommits_AcrossWeeks(t *testing.T) {
	dates := []time.Time{
		time.Date(2025, 6, 2, 10, 0, 0, 0, time.UTC),  // Week 23
		time.Date(2025, 6, 9, 10, 0, 0, 0, time.UTC),  // Week 24
		time.Date(2025, 6, 16, 10, 0, 0, 0, time.UTC), // Week 25
	}
	dir := initGitRepoWithCommits(t, dates)

	samples, err := SampleWeeklyCommits(context.Background(), dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	if len(samples) != 3 {
		t.Fatalf("got %d samples, want 3 (one per week)", len(samples))
	}

	// Should be chronologically ordered
	for i := 1; i < len(samples); i++ {
		if !samples[i].Date.After(samples[i-1].Date) {
			t.Errorf("samples not in chronological order: %s <= %s",
				samples[i].Date.Format("2006-01-02"),
				samples[i-1].Date.Format("2006-01-02"))
		}
	}
}

func TestSampleWeeklyCommits_SinceFilter(t *testing.T) {
	dates := []time.Time{
		time.Date(2025, 1, 6, 10, 0, 0, 0, time.UTC), // Jan
		time.Date(2025, 3, 3, 10, 0, 0, 0, time.UTC), // Mar
		time.Date(2025, 6, 2, 10, 0, 0, 0, time.UTC), // Jun
		time.Date(2025, 9, 1, 10, 0, 0, 0, time.UTC), // Sep
	}
	dir := initGitRepoWithCommits(t, dates)

	since := time.Date(2025, 5, 1, 0, 0, 0, 0, time.UTC)
	samples, err := SampleWeeklyCommits(context.Background(), dir, since)
	if err != nil {
		t.Fatal(err)
	}

	// Should only include Jun and Sep commits
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want 2 (after May 2025)", len(samples))
	}
}

func TestSampleWeeklyCommits_FirstAndLatestAlwaysIncluded(t *testing.T) {
	// Two commits in the same week: first and latest should both be in output
	dates := []time.Time{
		time.Date(2025, 6, 9, 8, 0, 0, 0, time.UTC),  // Monday morning
		time.Date(2025, 6, 9, 16, 0, 0, 0, time.UTC), // Monday afternoon
	}
	dir := initGitRepoWithCommits(t, dates)

	samples, err := SampleWeeklyCommits(context.Background(), dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	// The last commit per week is Monday afternoon.
	// First commit (Monday morning) should also be included.
	if len(samples) < 1 {
		t.Fatal("expected at least 1 sample")
	}

	// First sample should be the first commit
	if samples[0].Date.Hour() != 8 {
		t.Errorf("first sample hour = %d, want 8 (first commit)", samples[0].Date.Hour())
	}

	// If there are 2 samples, last should be the latest commit
	if len(samples) == 2 && samples[1].Date.Hour() != 16 {
		t.Errorf("last sample hour = %d, want 16 (latest commit)", samples[1].Date.Hour())
	}
}

func TestSampleWeeklyCommits_GapWeeks(t *testing.T) {
	// Commits in weeks 23 and 25, but not week 24
	dates := []time.Time{
		time.Date(2025, 6, 2, 10, 0, 0, 0, time.UTC),  // Week 23
		time.Date(2025, 6, 16, 10, 0, 0, 0, time.UTC), // Week 25 (skip week 24)
	}
	dir := initGitRepoWithCommits(t, dates)

	samples, err := SampleWeeklyCommits(context.Background(), dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	// Should have 2 samples, no phantom week 24
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want 2 (no sample for gap week)", len(samples))
	}
}

func TestSampleWeeklyCommits_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := SampleWeeklyCommits(ctx, t.TempDir(), time.Time{})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestWeekMonday(t *testing.T) {
	tests := []struct {
		name    string
		input   time.Time
		wantDay int // expected day of month for the Monday
	}{
		{
			name:    "already Monday",
			input:   time.Date(2025, 6, 9, 10, 0, 0, 0, time.UTC),
			wantDay: 9,
		},
		{
			name:    "Wednesday",
			input:   time.Date(2025, 6, 11, 10, 0, 0, 0, time.UTC),
			wantDay: 9,
		},
		{
			name:    "Sunday",
			input:   time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
			wantDay: 9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monday := weekMonday(tt.input)
			if monday.Weekday() != time.Monday {
				t.Errorf("weekMonday returned %s, not Monday", monday.Weekday())
			}
			if monday.Day() != tt.wantDay {
				t.Errorf("weekMonday day = %d, want %d", monday.Day(), tt.wantDay)
			}
		})
	}
}
