package leverage

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initGitRepo creates a temp git repo with user config and returns its path.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.name", "Alice"},
		{"git", "config", "user.email", "alice@example.com"},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s: %v", args, out, err)
		}
	}
	return dir
}

// commitFile creates (or overwrites) a file and commits it as the given author.
func commitFile(t *testing.T, dir, filename, content, authorName, authorEmail string) {
	t.Helper()

	path := filepath.Join(dir, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	commands := [][]string{
		{"git", "add", filename},
		{"git", "-c", "user.name=" + authorName, "-c", "user.email=" + authorEmail,
			"commit", "-m", "add " + filename, "--allow-empty"},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s: %v", args, out, err)
		}
	}
}

func TestGetAuthorLOC_SingleAuthor(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "main.go", "package main\n\nfunc main() {\n}\n", "Alice", "alice@example.com")

	authors, err := GetAuthorLOC(context.Background(), dir)
	if err != nil {
		t.Fatalf("GetAuthorLOC: %v", err)
	}

	if len(authors) != 1 {
		t.Fatalf("got %d authors, want 1", len(authors))
	}

	a := authors[0]
	if a.Name != "Alice" {
		t.Errorf("Name = %q, want Alice", a.Name)
	}
	if a.Email != "alice@example.com" {
		t.Errorf("Email = %q, want alice@example.com", a.Email)
	}
	if a.Lines != 4 {
		t.Errorf("Lines = %d, want 4", a.Lines)
	}
	if a.Percentage != 100.0 {
		t.Errorf("Percentage = %f, want 100.0", a.Percentage)
	}
}

func TestGetAuthorLOC_MultipleAuthors(t *testing.T) {
	dir := initGitRepo(t)

	// Alice writes 6 lines
	commitFile(t, dir, "a.go", "package a\n\nfunc A() {\n\treturn\n}\n\n", "Alice", "alice@example.com")

	// Bob writes 4 lines in a separate file
	commitFile(t, dir, "b.go", "package a\n\nfunc B() {\n}\n", "Bob", "bob@example.com")

	authors, err := GetAuthorLOC(context.Background(), dir)
	if err != nil {
		t.Fatalf("GetAuthorLOC: %v", err)
	}

	if len(authors) != 2 {
		t.Fatalf("got %d authors, want 2", len(authors))
	}

	// Should be sorted by lines descending: Alice (6) > Bob (4)
	if authors[0].Name != "Alice" {
		t.Errorf("authors[0].Name = %q, want Alice (most lines)", authors[0].Name)
	}
	if authors[0].Lines != 6 {
		t.Errorf("authors[0].Lines = %d, want 6", authors[0].Lines)
	}
	if authors[1].Name != "Bob" {
		t.Errorf("authors[1].Name = %q, want Bob", authors[1].Name)
	}
	if authors[1].Lines != 4 {
		t.Errorf("authors[1].Lines = %d, want 4", authors[1].Lines)
	}

	// Percentages should sum to 100
	totalPct := authors[0].Percentage + authors[1].Percentage
	if math.Abs(totalPct-100.0) > 0.01 {
		t.Errorf("percentages sum = %f, want 100.0", totalPct)
	}

	// Alice: 6/10 = 60%, Bob: 4/10 = 40%
	if math.Abs(authors[0].Percentage-60.0) > 0.01 {
		t.Errorf("Alice percentage = %f, want 60.0", authors[0].Percentage)
	}
	if math.Abs(authors[1].Percentage-40.0) > 0.01 {
		t.Errorf("Bob percentage = %f, want 40.0", authors[1].Percentage)
	}
}

func TestGetAuthorLOC_EmptyRepo(t *testing.T) {
	dir := initGitRepo(t)

	// Create an initial empty commit so git is valid
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s: %v", out, err)
	}

	authors, err := GetAuthorLOC(context.Background(), dir)
	if err != nil {
		t.Fatalf("GetAuthorLOC: %v", err)
	}

	if authors != nil {
		t.Errorf("expected nil authors for empty repo, got %v", authors)
	}
}

func TestGetAuthorLOC_ContextCancellation(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "main.go", "package main\n", "Alice", "alice@example.com")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := GetAuthorLOC(ctx, dir)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestGetAuthorLOC_MultipleFiles(t *testing.T) {
	dir := initGitRepo(t)

	// Alice writes two files (3 + 3 = 6 lines)
	commitFile(t, dir, "x.go", "package x\n\nvar X = 1\n", "Alice", "alice@example.com")
	commitFile(t, dir, "y.go", "package x\n\nvar Y = 2\n", "Alice", "alice@example.com")

	// Bob writes one file (3 lines)
	commitFile(t, dir, "z.go", "package x\n\nvar Z = 3\n", "Bob", "bob@example.com")

	authors, err := GetAuthorLOC(context.Background(), dir)
	if err != nil {
		t.Fatalf("GetAuthorLOC: %v", err)
	}

	if len(authors) != 2 {
		t.Fatalf("got %d authors, want 2", len(authors))
	}

	// Alice: 6 lines across 2 files, Bob: 3 lines in 1 file
	if authors[0].Name != "Alice" || authors[0].Lines != 6 {
		t.Errorf("authors[0] = {%s, %d}, want {Alice, 6}", authors[0].Name, authors[0].Lines)
	}
	if authors[1].Name != "Bob" || authors[1].Lines != 3 {
		t.Errorf("authors[1] = {%s, %d}, want {Bob, 3}", authors[1].Name, authors[1].Lines)
	}
}

func TestBuildContributions_Empty(t *testing.T) {
	result := buildContributions(nil)
	if result != nil {
		t.Errorf("expected nil for nil accum, got %v", result)
	}

	result = buildContributions(map[string]*authorAccum{})
	if result != nil {
		t.Errorf("expected nil for empty accum, got %v", result)
	}
}

func TestBuildContributions_ZeroLines(t *testing.T) {
	accum := map[string]*authorAccum{
		"a@test.com": {name: "A", email: "a@test.com", lines: 0},
	}
	result := buildContributions(accum)
	if result != nil {
		t.Errorf("expected nil for zero-line accum, got %v", result)
	}
}

func TestCountAuthors_SingleAuthor(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "main.go", "package main\n", "Alice", "alice@example.com")

	count, err := CountAuthors(context.Background(), dir)
	if err != nil {
		t.Fatalf("CountAuthors: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestCountAuthors_MultipleAuthors(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "a.go", "package a\n", "Alice", "alice@example.com")
	commitFile(t, dir, "b.go", "package a\n", "Bob", "bob@example.com")
	commitFile(t, dir, "c.go", "package a\n", "Charlie", "charlie@example.com")

	count, err := CountAuthors(context.Background(), dir)
	if err != nil {
		t.Fatalf("CountAuthors: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestCountAuthors_DeduplicatesSameName(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "a.go", "package a\n", "Alice", "alice@work.com")
	commitFile(t, dir, "b.go", "package b\n", "Alice", "alice@personal.com")

	count, err := CountAuthors(context.Background(), dir)
	if err != nil {
		t.Fatalf("CountAuthors: %v", err)
	}
	// Same name "Alice" with different emails = 1 person
	if count != 1 {
		t.Errorf("count = %d, want 1 (same name dedup)", count)
	}
}

func TestCountAuthors_FiltersBots(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "a.go", "package a\n", "Alice", "alice@example.com")
	commitFile(t, dir, "b.go", "package b\n", "dependabot[bot]", "dependabot[bot]@users.noreply.github.com")

	count, err := CountAuthors(context.Background(), dir)
	if err != nil {
		t.Fatalf("CountAuthors: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (bot should be filtered)", count)
	}
}

func TestCountAuthors_MinimumOne(t *testing.T) {
	dir := initGitRepo(t)
	// Empty repo with no commits: CountAuthors should return 1 as minimum
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s: %v", out, err)
	}

	count, err := CountAuthors(context.Background(), dir)
	if err != nil {
		t.Fatalf("CountAuthors: %v", err)
	}
	if count < 1 {
		t.Errorf("count = %d, want >= 1", count)
	}
}

func TestAuthorHasCommits(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "a.go", "package a\n", "Alice", "alice@example.com")
	commitFile(t, dir, "b.go", "package b\n", "Bob", "bob@example.com")

	ctx := context.Background()

	has, err := AuthorHasCommits(ctx, dir, "alice@example.com")
	if err != nil {
		t.Fatalf("AuthorHasCommits: %v", err)
	}
	if !has {
		t.Error("expected Alice to have commits")
	}

	has, err = AuthorHasCommits(ctx, dir, "unknown@example.com")
	if err != nil {
		t.Fatalf("AuthorHasCommits: %v", err)
	}
	if has {
		t.Error("expected unknown author to have no commits")
	}
}

func TestAuthorDateRange(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "a.go", "package a\n", "Alice", "alice@example.com")
	commitFile(t, dir, "b.go", "package b\n", "Alice", "alice@example.com")

	ctx := context.Background()
	first, last, err := AuthorDateRange(ctx, dir, "alice@example.com")
	if err != nil {
		t.Fatalf("AuthorDateRange: %v", err)
	}
	if first.IsZero() || last.IsZero() {
		t.Error("expected non-zero dates")
	}
	if last.Before(first) {
		t.Errorf("last (%v) should be >= first (%v)", last, first)
	}
}

func TestAuthorDateRange_NoCommits(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "a.go", "package a\n", "Alice", "alice@example.com")

	ctx := context.Background()
	_, _, err := AuthorDateRange(ctx, dir, "unknown@example.com")
	if err == nil {
		t.Error("expected error for author with no commits")
	}
}

func TestIsBotEmail(t *testing.T) {
	tests := []struct {
		email string
		want  bool
	}{
		{"alice@example.com", false},
		{"noreply@github.com", true},
		{"dependabot[bot]@users.noreply.github.com", true},
		{"renovate@whitesource.com", true},
		{"github-actions@github.com", true},
		{"bot@company.com", true},
		{"alice@company.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := isBotEmail(tt.email)
			if got != tt.want {
				t.Errorf("isBotEmail(%q) = %v, want %v", tt.email, got, tt.want)
			}
		})
	}
}

func TestParseNameEmail(t *testing.T) {
	tests := []struct {
		input     string
		wantName  string
		wantEmail string
	}{
		{"Alice <alice@example.com>", "Alice", "alice@example.com"},
		{"Bob Smith <bob@test.com>", "Bob Smith", "bob@test.com"},
		{"NoEmail", "NoEmail", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, email := parseNameEmail(tt.input)
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if email != tt.wantEmail {
				t.Errorf("email = %q, want %q", email, tt.wantEmail)
			}
		})
	}
}

func TestBuildContributions_Sorted(t *testing.T) {
	accum := map[string]*authorAccum{
		"c@test.com": {name: "C", email: "c@test.com", lines: 10},
		"a@test.com": {name: "A", email: "a@test.com", lines: 50},
		"b@test.com": {name: "B", email: "b@test.com", lines: 30},
	}

	result := buildContributions(accum)
	if len(result) != 3 {
		t.Fatalf("got %d results, want 3", len(result))
	}

	// Should be sorted by lines descending: A(50) > B(30) > C(10)
	if result[0].Name != "A" || result[0].Lines != 50 {
		t.Errorf("result[0] = {%s, %d}, want {A, 50}", result[0].Name, result[0].Lines)
	}
	if result[1].Name != "B" || result[1].Lines != 30 {
		t.Errorf("result[1] = {%s, %d}, want {B, 30}", result[1].Name, result[1].Lines)
	}
	if result[2].Name != "C" || result[2].Lines != 10 {
		t.Errorf("result[2] = {%s, %d}, want {C, 10}", result[2].Name, result[2].Lines)
	}

	// Percentages: A=55.56%, B=33.33%, C=11.11%
	totalPct := result[0].Percentage + result[1].Percentage + result[2].Percentage
	if math.Abs(totalPct-100.0) > 0.01 {
		t.Errorf("percentages sum = %f, want 100.0", totalPct)
	}
}
