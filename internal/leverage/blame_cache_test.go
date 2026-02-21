package leverage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlameCache_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	cache := NewBlameCache(dir)
	ctx := context.Background()

	entry := &BlameCacheEntry{
		Project:    "test-project",
		CommitHash: "abc123",
		SCCDir:     "/tmp/test",
		FileBlame: map[string]map[string]int{
			"main.go": {"alice@example.com": 10, "bob@example.com": 5},
			"util.go": {"alice@example.com": 20},
		},
		AuthorCount: 2,
		ActualPM:    3.5,
		Authors: []AuthorContribution{
			{Name: "Alice", Email: "alice@example.com", Lines: 30, Percentage: 66.67},
			{Name: "Bob", Email: "bob@example.com", Lines: 15, Percentage: 33.33},
		},
	}

	if err := cache.Save(ctx, entry); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := cache.Load(ctx, "test-project")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}

	if loaded.CommitHash != "abc123" {
		t.Errorf("CommitHash = %q, want abc123", loaded.CommitHash)
	}
	if loaded.AuthorCount != 2 {
		t.Errorf("AuthorCount = %d, want 2", loaded.AuthorCount)
	}
	if len(loaded.FileBlame) != 2 {
		t.Errorf("FileBlame has %d files, want 2", len(loaded.FileBlame))
	}
	if loaded.FileBlame["main.go"]["alice@example.com"] != 10 {
		t.Errorf("FileBlame[main.go][alice] = %d, want 10", loaded.FileBlame["main.go"]["alice@example.com"])
	}
	if len(loaded.Authors) != 2 {
		t.Errorf("Authors has %d entries, want 2", len(loaded.Authors))
	}
}

func TestBlameCache_LoadMissing(t *testing.T) {
	dir := t.TempDir()
	cache := NewBlameCache(dir)
	ctx := context.Background()

	entry, err := cache.Load(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil for missing cache, got %+v", entry)
	}
}

func TestBlameCache_LoadCorrupt(t *testing.T) {
	dir := t.TempDir()
	cache := NewBlameCache(dir)
	ctx := context.Background()

	// Write corrupt JSON.
	path := filepath.Join(dir, "bad-project.json")
	if err := os.WriteFile(path, []byte("not valid json{{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	entry, err := cache.Load(ctx, "bad-project")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if entry != nil {
		t.Errorf("expected nil for corrupt cache, got %+v", entry)
	}
}

func TestProjectHash_Standalone(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "main.go", "package main\n", "Alice", "alice@example.com")

	ctx := context.Background()
	p := &ResolvedProject{Name: "test", GitDir: dir, SCCDir: dir}

	hash, err := ProjectHash(ctx, p)
	if err != nil {
		t.Fatalf("ProjectHash: %v", err)
	}
	if len(hash) != 40 {
		t.Errorf("hash length = %d, want 40 (SHA-1)", len(hash))
	}

	// Hash should change after a new commit.
	commitFile(t, dir, "util.go", "package main\n", "Alice", "alice@example.com")
	hash2, err := ProjectHash(ctx, p)
	if err != nil {
		t.Fatalf("ProjectHash after commit: %v", err)
	}
	if hash2 == hash {
		t.Error("hash should change after new commit")
	}
}

func TestProjectHash_Monorepo(t *testing.T) {
	dir := initGitRepo(t)

	// Create subdir structure.
	subdir := filepath.Join(dir, "packages", "foo")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	commitFile(t, dir, "packages/foo/main.go", "package foo\n", "Alice", "alice@example.com")

	ctx := context.Background()
	p := &ResolvedProject{
		Name:       "foo",
		GitDir:     dir,
		SCCDir:     subdir,
		InMonorepo: true,
	}

	hash, err := ProjectHash(ctx, p)
	if err != nil {
		t.Fatalf("ProjectHash monorepo: %v", err)
	}
	if hash == "" {
		t.Error("expected non-empty tree hash")
	}

	// Hash should change when a file in the subdir changes.
	commitFile(t, dir, "packages/foo/util.go", "package foo\n", "Alice", "alice@example.com")
	hash2, err := ProjectHash(ctx, p)
	if err != nil {
		t.Fatalf("ProjectHash after subdir commit: %v", err)
	}
	if hash2 == hash {
		t.Error("tree hash should change after subdir commit")
	}
}

func TestChangedFiles(t *testing.T) {
	dir := initGitRepo(t)

	// Initial commit.
	commitFile(t, dir, "a.go", "package a\n", "Alice", "alice@example.com")
	commitFile(t, dir, "b.go", "package b\n", "Alice", "alice@example.com")

	// Capture hash after initial files.
	oldHash := gitHead(t, dir)

	// Make changes: modify a.go, add c.go, delete b.go.
	commitFile(t, dir, "a.go", "package a\nvar x = 1\n", "Alice", "alice@example.com")
	commitFile(t, dir, "c.go", "package c\n", "Alice", "alice@example.com")

	// Delete b.go.
	if err := os.Remove(filepath.Join(dir, "b.go")); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "-A")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s: %v", out, err)
	}
	cmd = exec.Command("git", "commit", "-m", "remove b.go")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s: %v", out, err)
	}

	newHash := gitHead(t, dir)

	ctx := context.Background()
	modified, added, deleted, err := ChangedFiles(ctx, dir, oldHash, newHash, "")
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}

	if !contains(modified, "a.go") {
		t.Errorf("expected a.go in modified, got %v", modified)
	}
	if !contains(added, "c.go") {
		t.Errorf("expected c.go in added, got %v", added)
	}
	if !contains(deleted, "b.go") {
		t.Errorf("expected b.go in deleted, got %v", deleted)
	}
}

func TestIncrementalUpdate(t *testing.T) {
	dir := initGitRepo(t)
	commitFile(t, dir, "a.go", "package a\nvar x = 1\n", "Alice", "alice@example.com")
	commitFile(t, dir, "b.go", "package b\nvar y = 1\n", "Bob", "bob@example.com")
	commitFile(t, dir, "c.go", "package c\n", "Alice", "alice@example.com")

	// Simulate an existing cache entry.
	entry := &BlameCacheEntry{
		FileBlame: map[string]map[string]int{
			"a.go": {"alice@example.com": 2},
			"b.go": {"bob@example.com": 2},
			"c.go": {"alice@example.com": 1},
		},
	}

	// Now modify a.go, delete c.go, add d.go.
	commitFile(t, dir, "a.go", "package a\nvar x = 1\nvar z = 2\n", "Alice", "alice@example.com")
	commitFile(t, dir, "d.go", "package d\nvar w = 1\n", "Bob", "bob@example.com")

	ctx := context.Background()
	err := entry.IncrementalUpdate(ctx, dir, []string{"a.go"}, []string{"d.go"}, []string{"c.go"})
	if err != nil {
		t.Fatalf("IncrementalUpdate: %v", err)
	}

	// c.go should be removed.
	if _, ok := entry.FileBlame["c.go"]; ok {
		t.Error("c.go should have been deleted from FileBlame")
	}

	// a.go should be updated (now 3 lines).
	if entry.FileBlame["a.go"]["alice@example.com"] != 3 {
		t.Errorf("a.go alice lines = %d, want 3", entry.FileBlame["a.go"]["alice@example.com"])
	}

	// d.go should be added.
	if _, ok := entry.FileBlame["d.go"]; !ok {
		t.Error("d.go should have been added to FileBlame")
	}
	if entry.FileBlame["d.go"]["bob@example.com"] != 2 {
		t.Errorf("d.go bob lines = %d, want 2", entry.FileBlame["d.go"]["bob@example.com"])
	}

	// b.go should be unchanged.
	if entry.FileBlame["b.go"]["bob@example.com"] != 2 {
		t.Errorf("b.go bob lines = %d, want 2 (unchanged)", entry.FileBlame["b.go"]["bob@example.com"])
	}
}

func TestRecomputeAggregates(t *testing.T) {
	entry := &BlameCacheEntry{
		FileBlame: map[string]map[string]int{
			"a.go": {"alice@example.com": 10, "bob@example.com": 5},
			"b.go": {"alice@example.com": 20},
			"c.go": {"bob@example.com": 15},
		},
	}

	entry.RecomputeAggregates()

	if entry.AuthorCount != 2 {
		t.Errorf("AuthorCount = %d, want 2", entry.AuthorCount)
	}
	if len(entry.Authors) != 2 {
		t.Fatalf("Authors has %d entries, want 2", len(entry.Authors))
	}

	// Alice: 30 lines (60%), Bob: 20 lines (40%).
	// Sorted by lines descending.
	if entry.Authors[0].Email != "alice@example.com" {
		t.Errorf("Authors[0].Email = %q, want alice@example.com", entry.Authors[0].Email)
	}
	if entry.Authors[0].Lines != 30 {
		t.Errorf("Authors[0].Lines = %d, want 30", entry.Authors[0].Lines)
	}
	if entry.Authors[1].Email != "bob@example.com" {
		t.Errorf("Authors[1].Email = %q, want bob@example.com", entry.Authors[1].Email)
	}
	if entry.Authors[1].Lines != 20 {
		t.Errorf("Authors[1].Lines = %d, want 20", entry.Authors[1].Lines)
	}
}

func TestRecomputeAggregates_WithEmailToName(t *testing.T) {
	entry := &BlameCacheEntry{
		FileBlame: map[string]map[string]int{
			"a.go": {"alice@example.com": 10, "bob@example.com": 5},
			"b.go": {"alice@example.com": 20},
		},
		EmailToName: map[string]string{
			"alice@example.com": "Alice Smith",
			"bob@example.com":   "Bob Jones",
		},
	}

	entry.RecomputeAggregates()

	if len(entry.Authors) != 2 {
		t.Fatalf("Authors has %d entries, want 2", len(entry.Authors))
	}

	// Verify names come from EmailToName, not email addresses.
	for _, a := range entry.Authors {
		if a.Name == a.Email {
			t.Errorf("Author name %q should not equal email — EmailToName mapping not applied", a.Name)
		}
	}
	if entry.Authors[0].Name != "Alice Smith" {
		t.Errorf("Authors[0].Name = %q, want %q", entry.Authors[0].Name, "Alice Smith")
	}
	if entry.Authors[1].Name != "Bob Jones" {
		t.Errorf("Authors[1].Name = %q, want %q", entry.Authors[1].Name, "Bob Jones")
	}
}

func TestRecomputeAggregates_NilEmailToName(t *testing.T) {
	// Backwards compat: old cache files without EmailToName should still work.
	entry := &BlameCacheEntry{
		FileBlame: map[string]map[string]int{
			"a.go": {"alice@example.com": 10},
		},
		EmailToName: nil,
	}

	entry.RecomputeAggregates()

	if len(entry.Authors) != 1 {
		t.Fatalf("Authors has %d entries, want 1", len(entry.Authors))
	}
	// Without mapping, name falls back to email.
	if entry.Authors[0].Name != "alice@example.com" {
		t.Errorf("Authors[0].Name = %q, want email fallback", entry.Authors[0].Name)
	}
}

func TestRecomputeAggregates_Empty(t *testing.T) {
	entry := &BlameCacheEntry{
		FileBlame: map[string]map[string]int{},
	}

	entry.RecomputeAggregates()

	if entry.AuthorCount != 0 {
		t.Errorf("AuthorCount = %d, want 0", entry.AuthorCount)
	}
	if entry.Authors != nil {
		t.Errorf("Authors should be nil, got %v", entry.Authors)
	}
}

func TestThreeTierIntegration(t *testing.T) {
	dir := initGitRepo(t)
	cacheDir := t.TempDir()
	cache := NewBlameCache(cacheDir)
	ctx := context.Background()

	// Create initial project state.
	commitFile(t, dir, "main.go", "package main\n\nfunc main() {\n}\n", "Alice", "alice@example.com")
	commitFile(t, dir, "util.go", "package main\n\nvar X = 1\n", "Bob", "bob@example.com")

	p := &ResolvedProject{Name: "test-proj", GitDir: dir, SCCDir: dir}

	// --- Tier C: Cold cache (full compute) ---
	hash1, err := ProjectHash(ctx, p)
	if err != nil {
		t.Fatalf("ProjectHash: %v", err)
	}

	cached, err := cache.Load(ctx, p.Name)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cached != nil {
		t.Fatal("expected nil for cold cache")
	}

	// Full compute.
	PopulateOneProjectCached(ctx, p, cache, hash1)

	if p.AuthorCount == 0 {
		t.Error("AuthorCount should be > 0 after full compute")
	}
	if p.ActualPersonMonths == 0 {
		t.Error("ActualPersonMonths should be > 0 after full compute")
	}

	// Verify cache was saved.
	cached, err = cache.Load(ctx, p.Name)
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if cached == nil {
		t.Fatal("cache should exist after full compute")
	}
	if cached.CommitHash != hash1 {
		t.Errorf("cached hash = %q, want %q", cached.CommitHash, hash1)
	}

	// --- Tier A: Exact hash match (cache hit) ---
	p2 := &ResolvedProject{Name: "test-proj", GitDir: dir, SCCDir: dir}
	hash2, err := ProjectHash(ctx, p2)
	if err != nil {
		t.Fatalf("ProjectHash: %v", err)
	}
	if hash2 != hash1 {
		t.Fatal("hash should not have changed")
	}

	// Populate from cache.
	p2.AuthorCount = cached.AuthorCount
	p2.ActualPersonMonths = cached.ActualPM
	p2.Authors = cached.Authors

	if p2.AuthorCount == 0 {
		t.Error("AuthorCount should be > 0 from cache")
	}

	// --- Tier B: Incremental update ---
	commitFile(t, dir, "new.go", "package main\n\nvar Y = 2\n", "Alice", "alice@example.com")

	hash3, err := ProjectHash(ctx, p)
	if err != nil {
		t.Fatalf("ProjectHash: %v", err)
	}
	if hash3 == hash1 {
		t.Fatal("hash should have changed after commit")
	}

	// Get changed files.
	modified, added, deleted, err := ChangedFiles(ctx, dir, cached.CommitHash, hash3, "")
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}

	totalChanged := len(modified) + len(added) + len(deleted)
	if totalChanged == 0 {
		t.Fatal("expected some changed files")
	}

	// Incremental update.
	if err := cached.IncrementalUpdate(ctx, dir, modified, added, deleted); err != nil {
		t.Fatalf("IncrementalUpdate: %v", err)
	}
	cached.RecomputeAggregates()
	cached.CommitHash = hash3

	// new.go should be in the cache.
	if _, ok := cached.FileBlame["new.go"]; !ok {
		t.Error("new.go should be in FileBlame after incremental update")
	}

	// Recompute project metrics.
	p3 := &ResolvedProject{Name: "test-proj", GitDir: dir, SCCDir: dir}
	RecomputeProjectMetrics(ctx, p3, cached)

	if p3.AuthorCount == 0 {
		t.Error("AuthorCount should be > 0 after incremental")
	}
}

// gitHead returns the current HEAD commit hash.
func gitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// contains checks if a string is in a slice.
func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
