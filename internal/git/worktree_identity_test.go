//go:build container_fs

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsWorktreeRoot_DoesNotClimbToParent(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q", root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	empty := filepath.Join(root, "empty-child")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{root, true},
		{empty, false},
		{filepath.Join(root, "missing-child"), false},
	} {
		got, err := IsWorktreeRoot(context.Background(), tc.path)
		if err != nil {
			t.Errorf("IsWorktreeRoot(%q): %v", tc.path, err)
		} else if got != tc.want {
			t.Errorf("IsWorktreeRoot(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
