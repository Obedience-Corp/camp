package workitem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCwdSubGitRepo_HonorsSymlinkedCampaignRoot(t *testing.T) {
	tmp := t.TempDir()
	realRoot := filepath.Join(tmp, "real")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	subRepo := filepath.Join(realRoot, "projects", "demo")
	if err := os.MkdirAll(filepath.Join(subRepo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	linkRoot := filepath.Join(tmp, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlink unsupported in this environment: %v", err)
	}

	cwdViaLink := filepath.Join(linkRoot, "projects", "demo")

	got, ok := cwdSubGitRepo(cwdViaLink, linkRoot)
	if !ok {
		t.Fatalf("cwdSubGitRepo should detect sub-repo via symlinked cwd; got ok=false")
	}
	wantCanonical, err := filepath.EvalSymlinks(subRepo)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantCanonical {
		t.Errorf("cwdSubGitRepo = %q, want canonical %q", got, wantCanonical)
	}
}
