package feedback

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScanner_Scan_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	scanner := NewScanner(dir)

	results, err := scanner.Scan(context.Background(), GatherOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestScanner_Scan_FindsFeedback(t *testing.T) {
	dir := setupTestFestivals(t)
	scanner := NewScanner(dir)

	results, err := scanner.Scan(context.Background(), GatherOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 festival with feedback, got %d", len(results))
	}

	fb := results[0]
	if fb.Festival.ID != "CC0004" {
		t.Errorf("expected festival ID CC0004, got %s", fb.Festival.ID)
	}
	if fb.Festival.Name != "camp-commands-enhancement" {
		t.Errorf("expected festival name camp-commands-enhancement, got %s", fb.Festival.Name)
	}
	if len(fb.Observations) != 2 {
		t.Errorf("expected 2 observations, got %d", len(fb.Observations))
	}
}

func TestScanner_Scan_FilterByFestivalID(t *testing.T) {
	dir := setupTestFestivals(t)
	scanner := NewScanner(dir)

	results, err := scanner.Scan(context.Background(), GatherOptions{FestivalID: "NONEXISTENT"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for nonexistent festival, got %d", len(results))
	}

	results, err = scanner.Scan(context.Background(), GatherOptions{FestivalID: "CC0004"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for CC0004, got %d", len(results))
	}
}

func TestScanner_Scan_FilterByStatus(t *testing.T) {
	dir := setupTestFestivals(t)
	scanner := NewScanner(dir)

	// Festival is in "completed" dir
	results, err := scanner.Scan(context.Background(), GatherOptions{Statuses: []string{"active"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for active status, got %d", len(results))
	}

	results, err = scanner.Scan(context.Background(), GatherOptions{Statuses: []string{"completed"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for completed status, got %d", len(results))
	}
}

func TestScanner_Scan_FilterBySeverity(t *testing.T) {
	dir := setupTestFestivals(t)
	scanner := NewScanner(dir)

	results, err := scanner.Scan(context.Background(), GatherOptions{Severity: "high"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 festival, got %d", len(results))
	}
	// Only obs 002 has severity "high"
	if len(results[0].Observations) != 1 {
		t.Errorf("expected 1 high-severity observation, got %d", len(results[0].Observations))
	}
}

func TestScanner_Scan_SkipsFestivalWithoutGoal(t *testing.T) {
	dir := t.TempDir()
	festDir := filepath.Join(dir, "completed", "no-goal")
	os.MkdirAll(filepath.Join(festDir, "feedback", "observations"), 0755)
	writeFile(t, filepath.Join(festDir, "feedback", "observations", "001.yaml"),
		"id: \"001\"\ncriteria: Test\nobservation: test obs\ntimestamp: \"2026-01-01T00:00:00Z\"\n")

	scanner := NewScanner(dir)
	results, err := scanner.Scan(context.Background(), GatherOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for festival without GOAL, got %d", len(results))
	}
}

func TestScanner_Scan_SkipsFestivalWithoutFeedback(t *testing.T) {
	dir := t.TempDir()
	festDir := filepath.Join(dir, "completed", "no-feedback")
	os.MkdirAll(festDir, 0755)
	writeFile(t, filepath.Join(festDir, "FESTIVAL_GOAL.md"),
		"---\nfest_type: festival\nfest_id: NF0001\nfest_name: no-feedback\nfest_status: completed\n---\n# No Feedback\n")

	scanner := NewScanner(dir)
	results, err := scanner.Scan(context.Background(), GatherOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for festival without feedback, got %d", len(results))
	}
}

func TestScanner_Scan_ContextCancellation(t *testing.T) {
	dir := setupTestFestivals(t)
	scanner := NewScanner(dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := scanner.Scan(ctx, GatherOptions{})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantID  string
		wantErr bool
	}{
		{
			name:    "valid frontmatter",
			input:   "---\nfest_type: festival\nfest_id: CC0004\nfest_name: test\n---\n# Title\n",
			wantID:  "CC0004",
			wantErr: false,
		},
		{
			name:    "no frontmatter",
			input:   "# Title\nSome content\n",
			wantErr: true,
		},
		{
			name:    "unterminated frontmatter",
			input:   "---\nfest_id: CC0004\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, err := parseFrontmatter([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fm.ID != tt.wantID {
				t.Errorf("expected ID %q, got %q", tt.wantID, fm.ID)
			}
		})
	}
}

// setupTestFestivals creates a temporary festivals directory structure for testing.
func setupTestFestivals(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create completed/camp-commands-enhancement-CC0004
	festDir := filepath.Join(dir, "completed", "camp-commands-enhancement-CC0004")
	obsDir := filepath.Join(festDir, "feedback", "observations")
	os.MkdirAll(obsDir, 0755)

	writeFile(t, filepath.Join(festDir, "FESTIVAL_GOAL.md"),
		"---\nfest_type: festival\nfest_id: CC0004\nfest_name: camp-commands-enhancement\nfest_status: completed\n---\n# camp-commands-enhancement\n")

	writeFile(t, filepath.Join(obsDir, "001.yaml"),
		"id: \"001\"\ncriteria: Unexpected behavior or confusing UX\nobservation: The graduate command is confusing\ntimestamp: \"2026-02-02T21:05:49Z\"\n")

	writeFile(t, filepath.Join(obsDir, "002.yaml"),
		"id: \"002\"\ncriteria: Missing features or functionality gaps\nobservation: INGEST should create PLANNING phase\ntimestamp: \"2026-02-05T08:06:09Z\"\nseverity: high\nsuggestion: Add automatic PLANNING phase creation\n")

	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
