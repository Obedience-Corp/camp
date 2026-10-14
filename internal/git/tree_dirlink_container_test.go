//go:build container_fs

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/dirlink"
)

func TestCaptureBlobsDoesNotFollowDirectoryLink(t *testing.T) {
	repo := t.TempDir()
	if err := exec.Command("git", "init", repo).Run(); err != nil {
		t.Skipf("git not available: %v", err)
	}
	external := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(filepath.Join(external, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(external, "nested", "secret.txt")
	if err := os.WriteFile(secret, []byte("outside tree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(repo, "projects", "linked")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := dirlink.Create(dirlink.CreateOptions{LinkPath: link, TargetPath: external}); err != nil {
		t.Fatal(err)
	}
	refs, err := CaptureBlobs(context.Background(), repo, []string{"projects/linked"})
	if err != nil {
		t.Fatalf("CaptureBlobs() = %v", err)
	}
	if len(refs) != 1 || refs[0].Mode != "120000" {
		t.Fatalf("CaptureBlobs() = %+v, want one symlink blob", refs)
	}
	for _, ref := range refs {
		if ref.Path == "projects/linked/nested/secret.txt" {
			t.Fatalf("followed external tree into capture: %+v", refs)
		}
	}
}
