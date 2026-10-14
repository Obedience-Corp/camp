//go:build container_fs

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/dirlink"
)

func TestCommitScoped_DirectoryLinkStagesOnlySymlink(t *testing.T) {
	repo := t.TempDir()
	external := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
		}
		return string(out)
	}
	for _, dir := range []string{repo, external} {
		git(dir, "init")
		git(dir, "config", "user.name", "Camp Test")
		git(dir, "config", "user.email", "camp-test@example.invalid")
	}
	secret := filepath.Join(external, "external-only.txt")
	if err := os.WriteFile(secret, []byte("outside the camp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(external, "add", "external-only.txt")
	git(external, "commit", "-m", "external seed")

	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", "seed.txt")
	git(repo, "commit", "-m", "camp seed")
	if err := os.WriteFile(filepath.Join(repo, "unrelated.txt"), []byte("staged elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", "unrelated.txt")
	if err := os.WriteFile(filepath.Join(repo, "inside.txt"), []byte("camp owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(repo, "projects", "linked")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := dirlink.Create(dirlink.CreateOptions{LinkPath: link, TargetPath: external}); err != nil {
		t.Fatal(err)
	}
	target, err := dirlink.ReadTarget(link)
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitScoped(context.Background(), repo, []string{"inside.txt", "projects/linked"}, &CommitOptions{Message: "link external project"}); err != nil {
		t.Fatalf("CommitScoped: %v", err)
	}

	tree := git(repo, "ls-tree", "-r", "HEAD")
	if !strings.Contains(tree, "120000 blob ") || !strings.Contains(tree, "\tprojects/linked\n") {
		t.Fatalf("linked project must be one mode 120000 blob, tree:\n%s", tree)
	}
	if strings.Contains(tree, "160000 commit ") || strings.Contains(tree, "projects/linked/") || strings.Contains(tree, "unrelated.txt") {
		t.Fatalf("scoped commit recorded a gitlink, external child, or unrelated file:\n%s", tree)
	}
	if !strings.Contains(tree, "\tinside.txt\n") {
		t.Fatalf("scoped commit lost ordinary named file:\n%s", tree)
	}
	if got := git(repo, "show", "HEAD:projects/linked"); got != target {
		t.Fatalf("symlink blob = %q, want link target %q", got, target)
	}
	if got := git(repo, "ls-files", "--stage", "--", "projects/linked"); !strings.HasPrefix(got, "120000 ") || !strings.HasSuffix(got, "\tprojects/linked\n") {
		t.Fatalf("index entry = %q, want one mode 120000 link", got)
	}
	if got := git(repo, "ls-files", "--stage", "--", "unrelated.txt"); !strings.Contains(got, "\tunrelated.txt\n") {
		t.Fatalf("unrelated staged file was lost: %q", got)
	}
	if got, err := os.ReadFile(secret); err != nil || string(got) != "outside the camp\n" {
		t.Fatalf("external source changed: %q, %v", got, err)
	}
}
