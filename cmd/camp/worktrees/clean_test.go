package worktrees

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStaleIfRemovable_GitdirMissing(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "orphan-wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /nonexistent/gitdir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cr, ok := staleIfRemovable("proj", "orphan-wt", wt)
	if !ok {
		t.Fatal("expected removable stale entry")
	}
	if cr.reason != "gitdir target does not exist" {
		t.Fatalf("reason = %q", cr.reason)
	}
	if cr.gitDirEntry {
		t.Fatal("should not mark as git dir entry")
	}
}

func TestStaleIfRemovable_HealthySkipped(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "healthy")
	gitdir := filepath.Join(dir, "real-gitdir")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(gitdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := staleIfRemovable("proj", "healthy", wt); ok {
		t.Fatal("healthy worktree must not be stale")
	}
}

func TestStaleIfRemovable_GitDirClone(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "clone")
	if err := os.MkdirAll(filepath.Join(wt, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cr, ok := staleIfRemovable("proj", "clone", wt)
	if !ok {
		t.Fatal("expected git-dir clone entry for skip path")
	}
	if !cr.gitDirEntry {
		t.Fatal("expected gitDirEntry")
	}
}
