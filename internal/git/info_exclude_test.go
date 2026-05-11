package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	cmd := exec.CommandContext(t.Context(), "git", "init", "-q", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func readExclude(t *testing.T, repo string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read exclude: %v", err)
	}
	return string(data)
}

func TestIsRepo(t *testing.T) {
	repo := initRepo(t)
	if !IsRepo(repo) {
		t.Errorf("IsRepo(%q) = false, want true", repo)
	}
	plain := t.TempDir()
	if IsRepo(plain) {
		t.Errorf("IsRepo(%q) = true for non-repo, want false", plain)
	}
}

func TestEnsureInfoExclude_AddsPattern(t *testing.T) {
	repo := initRepo(t)

	added, err := EnsureInfoExclude(context.Background(), repo, ".camp")
	if err != nil {
		t.Fatalf("EnsureInfoExclude: %v", err)
	}
	if !added {
		t.Fatal("added = false, want true on first add")
	}
	if got := readExclude(t, repo); !strings.Contains(got, ".camp") {
		t.Errorf("exclude does not contain .camp; got:\n%s", got)
	}
}

func TestEnsureInfoExclude_Idempotent(t *testing.T) {
	repo := initRepo(t)

	if _, err := EnsureInfoExclude(context.Background(), repo, ".camp"); err != nil {
		t.Fatalf("first EnsureInfoExclude: %v", err)
	}
	before := readExclude(t, repo)

	added, err := EnsureInfoExclude(context.Background(), repo, ".camp")
	if err != nil {
		t.Fatalf("second EnsureInfoExclude: %v", err)
	}
	if added {
		t.Error("added = true on second call, want false (already present)")
	}
	if after := readExclude(t, repo); after != before {
		t.Errorf("file mutated on idempotent call\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestEnsureInfoExclude_PreservesExistingPatterns(t *testing.T) {
	repo := initRepo(t)

	excludePath := filepath.Join(repo, ".git", "info", "exclude")
	if err := os.WriteFile(excludePath, []byte("*.tmp\nbuild/\n"), 0644); err != nil {
		t.Fatalf("seed exclude: %v", err)
	}

	if _, err := EnsureInfoExclude(context.Background(), repo, ".camp"); err != nil {
		t.Fatalf("EnsureInfoExclude: %v", err)
	}
	got := readExclude(t, repo)
	for _, want := range []string{"*.tmp", "build/", ".camp"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in exclude:\n%s", want, got)
		}
	}
}

func TestRemoveInfoExclude_RemovesPattern(t *testing.T) {
	repo := initRepo(t)

	if _, err := EnsureInfoExclude(context.Background(), repo, ".camp"); err != nil {
		t.Fatalf("EnsureInfoExclude: %v", err)
	}

	removed, err := RemoveInfoExclude(context.Background(), repo, ".camp")
	if err != nil {
		t.Fatalf("RemoveInfoExclude: %v", err)
	}
	if !removed {
		t.Fatal("removed = false, want true when pattern present")
	}
	if got := readExclude(t, repo); strings.Contains(got, ".camp") {
		t.Errorf("exclude still contains .camp:\n%s", got)
	}
}

func TestRemoveInfoExclude_MissingFile(t *testing.T) {
	repo := initRepo(t)
	// Force exclude file out of existence; git init usually leaves it
	// in place but be explicit.
	_ = os.Remove(filepath.Join(repo, ".git", "info", "exclude"))

	removed, err := RemoveInfoExclude(context.Background(), repo, ".camp")
	if err != nil {
		t.Fatalf("RemoveInfoExclude on missing file: %v", err)
	}
	if removed {
		t.Error("removed = true on missing file, want false")
	}
}

func TestRemoveInfoExclude_PatternNotPresent(t *testing.T) {
	repo := initRepo(t)

	excludePath := filepath.Join(repo, ".git", "info", "exclude")
	original := []byte("*.tmp\nbuild/\n")
	if err := os.WriteFile(excludePath, original, 0644); err != nil {
		t.Fatalf("seed exclude: %v", err)
	}

	removed, err := RemoveInfoExclude(context.Background(), repo, ".camp")
	if err != nil {
		t.Fatalf("RemoveInfoExclude: %v", err)
	}
	if removed {
		t.Error("removed = true when pattern absent, want false")
	}
	got, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("file rewritten on no-op removal\nwant: %q\ngot:  %q", original, got)
	}
}

func TestRemoveInfoExclude_PreservesOtherPatterns(t *testing.T) {
	repo := initRepo(t)

	excludePath := filepath.Join(repo, ".git", "info", "exclude")
	if err := os.WriteFile(excludePath, []byte("*.tmp\n.camp\nbuild/\n"), 0644); err != nil {
		t.Fatalf("seed exclude: %v", err)
	}

	if _, err := RemoveInfoExclude(context.Background(), repo, ".camp"); err != nil {
		t.Fatalf("RemoveInfoExclude: %v", err)
	}
	got := readExclude(t, repo)
	if strings.Contains(got, ".camp\n") {
		t.Errorf("exclude still contains .camp line:\n%s", got)
	}
	for _, want := range []string{"*.tmp", "build/"} {
		if !strings.Contains(got, want) {
			t.Errorf("removal stripped unrelated pattern %q:\n%s", want, got)
		}
	}
}

func TestEnsureInfoExclude_ContextCancelled(t *testing.T) {
	repo := initRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := EnsureInfoExclude(ctx, repo, ".camp"); err == nil {
		t.Error("expected error on cancelled context, got nil")
	}
}

func TestRemoveInfoExclude_ContextCancelled(t *testing.T) {
	repo := initRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := RemoveInfoExclude(ctx, repo, ".camp"); err == nil {
		t.Error("expected error on cancelled context, got nil")
	}
}
