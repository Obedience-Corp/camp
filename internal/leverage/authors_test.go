package leverage

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestAuthorLOC_SingleAuthor(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "main.go", "package main\n\nfunc main() {\n}\n", "Alice", "alice@example.com")

	authors, err := AuthorLOC(context.Background(), dir)
	if err != nil {
		t.Fatalf("AuthorLOC: %v", err)
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

func TestAuthorLOC_MultipleAuthors(t *testing.T) {
	dir := initGitRepo(t)

	// Alice writes 6 lines
	commitFile(t, dir, "a.go", "package a\n\nfunc A() {\n\treturn\n}\n\n", "Alice", "alice@example.com")

	// Bob writes 4 lines in a separate file
	commitFile(t, dir, "b.go", "package a\n\nfunc B() {\n}\n", "Bob", "bob@example.com")

	authors, err := AuthorLOC(context.Background(), dir)
	if err != nil {
		t.Fatalf("AuthorLOC: %v", err)
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

func TestAuthorLOC_EmptyRepo(t *testing.T) {
	dir := initGitRepo(t)

	// Create an initial empty commit so git is valid
	cmd := exec.Command("git", "commit", "--allow-empty", "-m", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s: %v", out, err)
	}

	authors, err := AuthorLOC(context.Background(), dir)
	if err != nil {
		t.Fatalf("AuthorLOC: %v", err)
	}

	if authors != nil {
		t.Errorf("expected nil authors for empty repo, got %v", authors)
	}
}

func TestAuthorLOC_ContextCancellation(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "main.go", "package main\n", "Alice", "alice@example.com")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := AuthorLOC(ctx, dir)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestAuthorLOC_MultipleFiles(t *testing.T) {
	dir := initGitRepo(t)

	// Alice writes two files (3 + 3 = 6 lines)
	commitFile(t, dir, "x.go", "package x\n\nvar X = 1\n", "Alice", "alice@example.com")
	commitFile(t, dir, "y.go", "package x\n\nvar Y = 2\n", "Alice", "alice@example.com")

	// Bob writes one file (3 lines)
	commitFile(t, dir, "z.go", "package x\n\nvar Z = 3\n", "Bob", "bob@example.com")

	authors, err := AuthorLOC(context.Background(), dir)
	if err != nil {
		t.Fatalf("AuthorLOC: %v", err)
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

	count, err := CountAuthors(context.Background(), dir, NewAuthorResolver(nil))
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

	count, err := CountAuthors(context.Background(), dir, NewAuthorResolver(nil))
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

	count, err := CountAuthors(context.Background(), dir, NewAuthorResolver(nil))
	if err != nil {
		t.Fatalf("CountAuthors: %v", err)
	}
	// With fallback resolver (email-as-ID), different emails = different people.
	// Same-name dedup only happens with an explicit AuthorConfig.
	if count != 2 {
		t.Errorf("count = %d, want 2 (different emails → different IDs with fallback resolver)", count)
	}
}

func TestCountAuthors_SameEmailDifferentNames(t *testing.T) {
	dir := initGitRepo(t)
	// Same email, different display names (the real-world bug)
	commitFile(t, dir, "a.go", "package a\n", "lancekrogers", "lancekrogers@gmail.com")
	commitFile(t, dir, "b.go", "package b\n", "Lance Rogers", "lancekrogers@gmail.com")

	count, err := CountAuthors(context.Background(), dir, NewAuthorResolver(nil))
	if err != nil {
		t.Fatalf("CountAuthors: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1 (same email, different display names)", count)
	}
}

func TestCountAuthors_TransitiveMerge(t *testing.T) {
	dir := initGitRepo(t)
	// With the fallback resolver (email-as-ID), each unique email is a separate author.
	// Transitive merges by name only happen with an explicit AuthorConfig.
	commitFile(t, dir, "a.go", "package a\n", "Alice", "alice@work.com")
	commitFile(t, dir, "b.go", "package b\n", "Alice", "alice@personal.com")
	commitFile(t, dir, "c.go", "package c\n", "A. Smith", "alice@personal.com")

	count, err := CountAuthors(context.Background(), dir, NewAuthorResolver(nil))
	if err != nil {
		t.Fatalf("CountAuthors: %v", err)
	}
	// Fallback resolver: alice@work.com and alice@personal.com are separate IDs.
	if count != 2 {
		t.Errorf("count = %d, want 2 (fallback resolver: each unique email is a separate author)", count)
	}
}

func TestCountAuthors_FiltersBots(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "a.go", "package a\n", "Alice", "alice@example.com")
	commitFile(t, dir, "b.go", "package b\n", "dependabot[bot]", "dependabot[bot]@users.noreply.github.com")

	count, err := CountAuthors(context.Background(), dir, NewAuthorResolver(nil))
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

	count, err := CountAuthors(context.Background(), dir, NewAuthorResolver(nil))
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

// commitFileWithDate creates a file and commits it with a specific date.
func commitFileWithDate(t *testing.T, dir, filename, content, authorName, authorEmail, date string) {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	addCmd := exec.Command("git", "add", filename)
	addCmd.Dir = dir
	if out, err := addCmd.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %s: %v", out, err)
	}

	commitCmd := exec.Command("git",
		"-c", "user.name="+authorName,
		"-c", "user.email="+authorEmail,
		"commit", "-m", "add "+filename, "--allow-empty")
	commitCmd.Dir = dir
	commitCmd.Env = append(os.Environ(),
		"GIT_COMMITTER_DATE="+date,
		"GIT_AUTHOR_DATE="+date,
	)
	if out, err := commitCmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %s: %v", out, err)
	}
}

func TestProjectActualPersonMonths_SingleAuthor(t *testing.T) {
	dir := initGitRepo(t)

	// Alice commits over 3 months
	commitFileWithDate(t, dir, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, dir, "b.go", "package a\n", "Alice", "alice@example.com", "2025-04-01T12:00:00+00:00")

	pm, err := ProjectActualPersonMonths(context.Background(), dir, dir, NewAuthorResolver(nil))
	if err != nil {
		t.Fatalf("ProjectActualPersonMonths: %v", err)
	}

	// Should be ~3 months for one author
	if pm < 2.5 || pm > 3.5 {
		t.Errorf("pm = %.2f, want ~3.0 (one author active 3 months)", pm)
	}
}

func TestProjectActualPersonMonths_TwoAuthors_DifferentDurations(t *testing.T) {
	dir := initGitRepo(t)

	// Alice active for 6 months (Jan-Jul)
	commitFileWithDate(t, dir, "a1.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, dir, "a2.go", "package a\nvar x = 1\n", "Alice", "alice@example.com", "2025-07-01T12:00:00+00:00")

	// Bob active for 1 month only (Mar-Apr)
	commitFileWithDate(t, dir, "b1.go", "package a\nvar y = 1\n", "Bob", "bob@example.com", "2025-03-01T12:00:00+00:00")
	commitFileWithDate(t, dir, "b2.go", "package a\nvar z = 1\n", "Bob", "bob@example.com", "2025-04-01T12:00:00+00:00")

	pm, err := ProjectActualPersonMonths(context.Background(), dir, dir, NewAuthorResolver(nil))
	if err != nil {
		t.Fatalf("ProjectActualPersonMonths: %v", err)
	}

	// Blame-weighted: Alice ~43% LOC × 6mo ≈ 2.57, Bob ~57% LOC × 1mo ≈ 0.57.
	// Total ≈ 3.1 PM (much less than unweighted 7.0 because Bob's wide span
	// is now weighted by his small LOC share... wait, Bob has MORE lines here).
	// Alice: 3 lines (a1.go 1 line + a2.go 2 lines)
	// Bob: 4 lines (b1.go 2 lines + b2.go 2 lines)
	// Alice: 3/7 = 42.9% × 6mo = 2.57, Bob: 4/7 = 57.1% × 1mo = 0.57
	// Total ≈ 3.14 PM
	if pm < 2.5 || pm > 4.0 {
		t.Errorf("pm = %.2f, want ~3.1 (blame-weighted: Alice 43%%×6mo + Bob 57%%×1mo)", pm)
	}

	// Verify this is LESS than naive calculation (2 * 6 = 12)
	naivePM := 2.0 * 6.0
	if pm >= naivePM {
		t.Errorf("blame-weighted PM (%.2f) should be less than naive (%.2f)", pm, naivePM)
	}
}

func TestProjectActualPersonMonths_SingleCommitAuthor(t *testing.T) {
	dir := initGitRepo(t)

	// Author with a single commit should get minimum 0.1 months
	commitFileWithDate(t, dir, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")

	pm, err := ProjectActualPersonMonths(context.Background(), dir, dir, NewAuthorResolver(nil))
	if err != nil {
		t.Fatalf("ProjectActualPersonMonths: %v", err)
	}

	if pm < 0.1 || pm > 0.2 {
		t.Errorf("pm = %.2f, want ~0.1 (single commit = minimum)", pm)
	}
}

func TestProjectActualPersonMonths_ContextCancelled(t *testing.T) {
	dir := initGitRepo(t)
	commitFileWithDate(t, dir, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ProjectActualPersonMonths(ctx, dir, dir, NewAuthorResolver(nil))
	if err == nil {
		t.Fatal("expected error from cancelled context")
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

func TestGitDirAuthors(t *testing.T) {
	dir := initGitRepo(t)
	commitFileWithDate(t, dir, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, dir, "b.go", "package a\nvar x = 1\n", "Alice", "alice@example.com", "2025-06-01T12:00:00+00:00")
	commitFileWithDate(t, dir, "c.go", "package a\nvar y = 1\n", "Bob", "bob@example.com", "2025-03-01T12:00:00+00:00")

	resolver := NewAuthorResolver(nil)
	authors, err := gitDirAuthors(context.Background(), dir, resolver)
	if err != nil {
		t.Fatalf("gitDirAuthors: %v", err)
	}

	if len(authors) != 2 {
		t.Fatalf("got %d authors, want 2", len(authors))
	}

	// With fallback resolver, keys are lowercase emails.
	alice, ok := authors["alice@example.com"]
	if !ok {
		t.Fatal("missing alice@example.com")
	}
	if alice.earliest.IsZero() || alice.latest.IsZero() {
		t.Error("alice dates should be non-zero")
	}

	bob, ok := authors["bob@example.com"]
	if !ok {
		t.Fatal("missing bob@example.com")
	}
	if bob.earliest.IsZero() {
		t.Error("bob earliest should be non-zero")
	}
}

func TestCampaignActualPersonMonths_DeduplicatesAcrossRepos(t *testing.T) {
	ctx := context.Background()

	repo1 := initGitRepo(t)
	repo2 := initGitRepo(t)

	// Alice commits to repo1: Jan - Sep (8 months)
	commitFileWithDate(t, repo1, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, repo1, "b.go", "package a\nvar x = 1\n", "Alice", "alice@example.com", "2025-09-01T12:00:00+00:00")

	// Alice also commits to repo2: Mar - Jul (within the same span)
	commitFileWithDate(t, repo2, "c.go", "package b\n", "Alice", "alice@example.com", "2025-03-01T12:00:00+00:00")
	commitFileWithDate(t, repo2, "d.go", "package b\nvar y = 1\n", "Alice", "alice@example.com", "2025-07-01T12:00:00+00:00")

	projects := []ResolvedProject{
		{Name: "project1", GitDir: repo1, SCCDir: repo1},
		{Name: "project2", GitDir: repo2, SCCDir: repo2},
	}

	resolver := NewAuthorResolver(nil)
	campaignPM, err := CampaignActualPersonMonths(ctx, projects, resolver)
	if err != nil {
		t.Fatalf("CampaignActualPersonMonths: %v", err)
	}

	// Alice's campaign span is Jan-Sep = ~8 months, NOT repo1(8) + repo2(4) = 12
	if campaignPM < 7.5 || campaignPM > 9.0 {
		t.Errorf("campaignPM = %.2f, want ~8.0 (deduplicated across repos)", campaignPM)
	}

	// Must be less than naive sum of per-project PMs
	pm1, _ := ProjectActualPersonMonths(ctx, repo1, repo1, resolver)
	pm2, _ := ProjectActualPersonMonths(ctx, repo2, repo2, resolver)
	naiveSum := pm1 + pm2
	if campaignPM >= naiveSum {
		t.Errorf("campaign PM (%.2f) should be less than naive sum (%.2f)", campaignPM, naiveSum)
	}
}

func TestCampaignActualPersonMonths_DeduplicatesMonorepo(t *testing.T) {
	ctx := context.Background()

	// Single repo, two subprojects pointing to same GitDir
	repo := initGitRepo(t)
	commitFileWithDate(t, repo, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, repo, "b.go", "package a\nvar x = 1\n", "Alice", "alice@example.com", "2025-07-01T12:00:00+00:00")

	projects := []ResolvedProject{
		{Name: "sub1", GitDir: repo, SCCDir: repo + "/sub1", InMonorepo: true},
		{Name: "sub2", GitDir: repo, SCCDir: repo + "/sub2", InMonorepo: true},
	}

	resolver := NewAuthorResolver(nil)
	campaignPM, err := CampaignActualPersonMonths(ctx, projects, resolver)
	if err != nil {
		t.Fatalf("CampaignActualPersonMonths: %v", err)
	}

	// Single author, ~6 months. Should NOT be doubled.
	singlePM, _ := ProjectActualPersonMonths(ctx, repo, repo, resolver)
	if math.Abs(campaignPM-singlePM) > 0.5 {
		t.Errorf("campaignPM = %.2f, singlePM = %.2f; monorepo should not double-count", campaignPM, singlePM)
	}
}

func TestCampaignActualPersonMonths_MultipleAuthors(t *testing.T) {
	ctx := context.Background()

	repo1 := initGitRepo(t)
	repo2 := initGitRepo(t)

	// Alice in repo1: Jan-Sep (~8 months)
	commitFileWithDate(t, repo1, "a1.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, repo1, "a2.go", "package a\nvar x = 1\n", "Alice", "alice@example.com", "2025-09-01T12:00:00+00:00")

	// Bob in repo1: Jan-Mar (~2 months)
	commitFileWithDate(t, repo1, "b1.go", "package a\nvar y = 1\n", "Bob", "bob@example.com", "2025-01-15T12:00:00+00:00")
	commitFileWithDate(t, repo1, "b2.go", "package a\nvar z = 1\n", "Bob", "bob@example.com", "2025-03-15T12:00:00+00:00")

	// Alice in repo2: Mar-Jul (within her repo1 span)
	commitFileWithDate(t, repo2, "c1.go", "package b\n", "Alice", "alice@example.com", "2025-03-01T12:00:00+00:00")
	commitFileWithDate(t, repo2, "c2.go", "package b\nvar w = 1\n", "Alice", "alice@example.com", "2025-07-01T12:00:00+00:00")

	projects := []ResolvedProject{
		{Name: "project1", GitDir: repo1, SCCDir: repo1},
		{Name: "project2", GitDir: repo2, SCCDir: repo2},
	}

	campaignPM, err := CampaignActualPersonMonths(ctx, projects, NewAuthorResolver(nil))
	if err != nil {
		t.Fatalf("CampaignActualPersonMonths: %v", err)
	}

	// Blame-weighted across campaign:
	// Alice: 6 lines total (3 in repo1 + 3 in repo2) = 60%
	// Bob: 4 lines total (in repo1) = 40%
	// Alice PM: 8mo × 0.6 = 4.8, Bob PM: 2mo × 0.4 = 0.8
	// Total ≈ 5.6 PM
	if campaignPM < 4.5 || campaignPM > 7.0 {
		t.Errorf("campaignPM = %.2f, want ~5.6 (blame-weighted: Alice 60%%×8mo + Bob 40%%×2mo)", campaignPM)
	}
}

func TestCampaignActualPersonMonths_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	projects := []ResolvedProject{{Name: "p", GitDir: "/tmp/nonexistent"}}
	_, err := CampaignActualPersonMonths(ctx, projects, NewAuthorResolver(nil))
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// --- BlameWeightedPersonMonths tests ---

func TestBlameWeightedPersonMonths_SingleAuthor(t *testing.T) {
	dir := initGitRepo(t)
	commitFileWithDate(t, dir, "main.go", "package main\n\nfunc main() {\n}\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, dir, "util.go", "package main\n\nvar X = 1\n", "Alice", "alice@example.com", "2025-04-01T12:00:00+00:00")

	pm, authors, err := BlameWeightedPersonMonths(context.Background(), dir, dir, NewAuthorResolver(nil))
	if err != nil {
		t.Fatalf("BlameWeightedPersonMonths: %v", err)
	}

	// Single author = 100% LOC × ~3 months ≈ 3.0 PM
	if pm < 2.5 || pm > 3.5 {
		t.Errorf("pm = %.2f, want ~3.0 (100%% × 3 months)", pm)
	}

	if len(authors) != 1 {
		t.Fatalf("got %d authors, want 1", len(authors))
	}

	a := authors[0]
	if a.WeightedPM <= 0 {
		t.Errorf("WeightedPM = %.4f, want > 0", a.WeightedPM)
	}
	if math.Abs(a.Percentage-100.0) > 0.01 {
		t.Errorf("Percentage = %.2f, want 100.0", a.Percentage)
	}
}

func TestBlameWeightedPersonMonths_MixedContributions(t *testing.T) {
	dir := initGitRepo(t)

	// Alice: 6 lines across 2 files, active Jan-Jul (6 months)
	commitFileWithDate(t, dir, "a.go", "package a\n\nfunc A() {\n\treturn\n}\n\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, dir, "a2.go", "package a\n", "Alice", "alice@example.com", "2025-07-01T12:00:00+00:00")

	// Bob: 4 lines in 1 file, active Mar-Apr (1 month)
	commitFileWithDate(t, dir, "b.go", "package a\n\nfunc B() {\n}\n", "Bob", "bob@example.com", "2025-03-01T12:00:00+00:00")
	commitFileWithDate(t, dir, "b2.go", "package a\n", "Bob", "bob@example.com", "2025-04-01T12:00:00+00:00")

	pm, authors, err := BlameWeightedPersonMonths(context.Background(), dir, dir, NewAuthorResolver(nil))
	if err != nil {
		t.Fatalf("BlameWeightedPersonMonths: %v", err)
	}

	// Each author's PM is their months × their LOC share.
	// Total should be significantly less than unweighted (6 + 1 = 7).
	if pm >= 7.0 {
		t.Errorf("blame-weighted PM (%.2f) should be less than unweighted sum (7.0)", pm)
	}

	if len(authors) < 2 {
		t.Fatalf("got %d authors, want >= 2", len(authors))
	}

	// All authors should have WeightedPM set
	for _, a := range authors {
		if a.WeightedPM <= 0 {
			t.Errorf("author %s has WeightedPM = %.4f, want > 0", a.Name, a.WeightedPM)
		}
	}
}

func TestBlameWeightedPersonMonths_FiltersBelowOnePercent(t *testing.T) {
	dir := initGitRepo(t)

	// Alice: writes 200+ lines (dominant contributor)
	var bigContent string
	for i := range 200 {
		bigContent += fmt.Sprintf("var v%d = %d\n", i, i)
	}
	commitFileWithDate(t, dir, "big.go", "package a\n\n"+bigContent, "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, dir, "big2.go", "package a\n", "Alice", "alice@example.com", "2025-06-01T12:00:00+00:00")

	// Bob: writes just 1 line (<1% of 203 total) but over 8 months
	commitFileWithDate(t, dir, "tiny.go", "package a\n", "Bob", "bob@example.com", "2025-01-01T12:00:00+00:00")
	commitFileWithDate(t, dir, "tiny2.go", "package a\n", "Bob", "bob@example.com", "2025-09-01T12:00:00+00:00")

	pm, authors, err := BlameWeightedPersonMonths(context.Background(), dir, dir, NewAuthorResolver(nil))
	if err != nil {
		t.Fatalf("BlameWeightedPersonMonths: %v", err)
	}

	// Bob should be filtered out (<1% LOC), so only Alice appears.
	for _, a := range authors {
		if a.Name == "Bob" {
			t.Errorf("Bob should be filtered out (<1%% LOC), but got: %+v", a)
		}
	}

	// Without Bob's 8-month span inflating the total, PM should be ~5 months (Alice only).
	if pm > 6.0 {
		t.Errorf("pm = %.2f, should be ~5 (Alice only, Bob filtered)", pm)
	}
	_ = authors // satisfy vet
}

func TestBlameWeightedPersonMonths_SingleCommitAuthor(t *testing.T) {
	dir := initGitRepo(t)
	commitFileWithDate(t, dir, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")

	pm, _, err := BlameWeightedPersonMonths(context.Background(), dir, dir, NewAuthorResolver(nil))
	if err != nil {
		t.Fatalf("BlameWeightedPersonMonths: %v", err)
	}

	// Single commit author gets minimum PM (0.1)
	if pm < 0.09 || pm > 0.2 {
		t.Errorf("pm = %.4f, want ~0.1 (single commit minimum)", pm)
	}
}

func TestBlameWeightedPersonMonths_ContextCancelled(t *testing.T) {
	dir := initGitRepo(t)
	commitFileWithDate(t, dir, "a.go", "package a\n", "Alice", "alice@example.com", "2025-01-01T12:00:00+00:00")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := BlameWeightedPersonMonths(ctx, dir, dir, NewAuthorResolver(nil))
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestAuthorContribution_WeightedPM_BackwardCompat(t *testing.T) {
	// Old snapshots without WeightedPM should deserialize correctly.
	oldJSON := `{"name":"Alice","email":"alice@test.com","lines":100,"percentage":50.0}`
	var ac AuthorContribution
	if err := json.Unmarshal([]byte(oldJSON), &ac); err != nil {
		t.Fatalf("unmarshal old snapshot: %v", err)
	}
	if ac.WeightedPM != 0 {
		t.Errorf("WeightedPM = %f, want 0 for old snapshot", ac.WeightedPM)
	}
	if ac.Name != "Alice" || ac.Lines != 100 {
		t.Errorf("basic fields corrupted: %+v", ac)
	}

	// New snapshots with WeightedPM should round-trip.
	newJSON := `{"name":"Bob","email":"bob@test.com","lines":200,"percentage":75.0,"weighted_pm":3.5}`
	var ac2 AuthorContribution
	if err := json.Unmarshal([]byte(newJSON), &ac2); err != nil {
		t.Fatalf("unmarshal new snapshot: %v", err)
	}
	if ac2.WeightedPM != 3.5 {
		t.Errorf("WeightedPM = %f, want 3.5", ac2.WeightedPM)
	}
}
