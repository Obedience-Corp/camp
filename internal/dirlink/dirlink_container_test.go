//go:build container_fs

package dirlink

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndReadDirectoryLink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "external")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "projects", "linked")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Create(CreateOptions{LinkPath: link, TargetPath: target}); err != nil {
		t.Fatalf("Create() = %v", err)
	}
	st, err := Lstat(link)
	if err != nil || !st.IsDirectoryLink() {
		t.Fatalf("Lstat() = %+v, %v; want directory link", st, err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil || resolved != target {
		t.Fatalf("EvalSymlinks() = %q, %v; want %q", resolved, err, target)
	}
}

func TestRemoveDirectoryLinkLeavesTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "external")
	marker := filepath.Join(target, "keep.txt")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("stay"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := Create(CreateOptions{LinkPath: link, TargetPath: target}); err != nil {
		t.Fatal(err)
	}
	if err := Remove(link); err != nil {
		t.Fatalf("Remove() = %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("target file removed: %v", err)
	}
}

func TestBrokenDirectoryLinkState(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "gone")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "broken")
	if err := Create(CreateOptions{LinkPath: link, TargetPath: target, StoreTarget: target}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	raw, err := ReadTarget(link)
	if err != nil {
		t.Fatalf("ReadTarget() = %v", err)
	}
	if raw == "" {
		t.Fatal("expected stored target on broken link")
	}
}
