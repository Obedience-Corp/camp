package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolvePathThroughExistingAncestorResolvesSymlinkAncestor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior is platform dependent on Windows")
	}

	root := t.TempDir()
	realSrc := filepath.Join(root, "real-src")
	if err := os.MkdirAll(filepath.Join(realSrc, "child"), 0755); err != nil {
		t.Fatalf("failed to create real source: %v", err)
	}
	linkSrc := filepath.Join(root, "link-src")
	if err := os.Symlink(realSrc, linkSrc); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	got, err := resolvePathThroughExistingAncestor(filepath.Join(linkSrc, "child", "new-dir"))
	if err != nil {
		t.Fatalf("resolvePathThroughExistingAncestor() error = %v", err)
	}
	resolvedRealSrc, err := filepath.EvalSymlinks(realSrc)
	if err != nil {
		t.Fatalf("EvalSymlinks(realSrc) error = %v", err)
	}
	want := filepath.Join(resolvedRealSrc, "child", "new-dir")
	if got != want {
		t.Fatalf("resolvePathThroughExistingAncestor() = %q, want %q", got, want)
	}
}
