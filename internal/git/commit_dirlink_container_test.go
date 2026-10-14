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
	"github.com/Obedience-Corp/camp/internal/stageguard"
)

func TestCommitScoped_DirectoryLinkStagesOnlySymlink(t *testing.T) {
	f := newLinkedGitFixture(t)
	repo, external, git := f.repo, f.external, f.git
	if err := os.WriteFile(filepath.Join(repo, "unrelated.txt"), []byte("staged elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", "unrelated.txt")
	if err := os.WriteFile(filepath.Join(repo, "inside.txt"), []byte("camp owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	target, err := dirlink.ReadTarget(f.link)
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
	if got, err := os.ReadFile(filepath.Join(external, "external-only.txt")); err != nil || string(got) != "outside the camp\n" {
		t.Fatalf("external source changed: %q, %v", got, err)
	}
}

func TestStageAllDirectoryLinkStagesSymlink(t *testing.T) {
	f := newLinkedGitFixture(t)
	if err := os.WriteFile(filepath.Join(f.repo, "inside.txt"), []byte("camp owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(f.repo, "other-nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	f.git(nested, "init")
	f.git(nested, "config", "user.name", "Camp Test")
	f.git(nested, "config", "user.email", "camp-test@example.invalid")
	if err := os.WriteFile(filepath.Join(nested, "nested.txt"), []byte("nested checkout\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.git(nested, "add", "nested.txt")
	f.git(nested, "commit", "-m", "nested seed")
	limits, err := stageguard.ResolveLimits(context.Background(), f.repo)
	if err != nil {
		t.Fatal(err)
	}
	violations, err := CheckStaging(context.Background(), f.repo, limits)
	if err != nil {
		t.Fatalf("CheckStaging: %v", err)
	}
	if len(violations) != 1 || violations[0].Kind != stageguard.NestedRepo || violations[0].Path != "other-nested" {
		t.Fatalf("preflight should report only real nested repo, got %+v", violations)
	}
	outcome, err := StageAllExcludingWithGuardOptions(context.Background(), f.repo, nil, StageOptions{})
	if err != nil {
		t.Fatalf("StageAllExcludingWithGuardOptions: %v", err)
	}
	if outcome == nil || len(outcome.NestedRepos) != 1 || outcome.NestedRepos[0].Path != "other-nested" {
		t.Fatalf("nested repo reporting must exclude only real checkout, got %+v", outcome)
	}
	if err := Commit(context.Background(), f.repo, &CommitOptions{Message: "stage all camp changes"}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	f.assertCommittedLink(t)
	if tree := f.git(f.repo, "ls-tree", "-r", "HEAD"); !strings.Contains(tree, "\tinside.txt\n") || strings.Contains(tree, "other-nested") {
		t.Fatalf("sweep lost ordinary camp file or staged undeclared nested repo:\n%s", tree)
	}
}

func TestStageAllOnlyDirectoryLink(t *testing.T) {
	f := newLinkedGitFixture(t)
	if err := StageAll(context.Background(), f.repo); err != nil {
		t.Fatalf("StageAll with link as sole change: %v", err)
	}
	if err := Commit(context.Background(), f.repo, &CommitOptions{Message: "link only"}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	f.assertCommittedLink(t)
}

func TestCommitScopedParentDirectoryStagesLink(t *testing.T) {
	f := newLinkedGitFixture(t)
	if err := os.WriteFile(filepath.Join(f.repo, "projects", "local.txt"), []byte("camp owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CommitScoped(context.Background(), f.repo, []string{"projects"}, &CommitOptions{Message: "commit projects"}); err != nil {
		t.Fatalf("CommitScoped projects: %v", err)
	}
	f.assertCommittedLink(t)
	if tree := f.git(f.repo, "ls-tree", "-r", "HEAD"); !strings.Contains(tree, "\tprojects/local.txt\n") {
		t.Fatalf("parent scope lost ordinary child:\n%s", tree)
	}
}

type linkedGitFixture struct {
	repo, external, link string
	git                  func(dir string, args ...string) string
}

func newLinkedGitFixture(t *testing.T) linkedGitFixture {
	t.Helper()
	repo, external := t.TempDir(), t.TempDir()
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
	if err := os.WriteFile(filepath.Join(external, "external-only.txt"), []byte("outside the camp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(external, "add", "external-only.txt")
	git(external, "commit", "-m", "external seed")
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", "seed.txt")
	git(repo, "commit", "-m", "camp seed")
	link := filepath.Join(repo, "projects", "linked")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := dirlink.Create(dirlink.CreateOptions{LinkPath: link, TargetPath: external}); err != nil {
		t.Fatal(err)
	}
	return linkedGitFixture{repo: repo, external: external, link: link, git: git}
}

func (f linkedGitFixture) assertCommittedLink(t *testing.T) {
	t.Helper()
	tree := f.git(f.repo, "ls-tree", "-r", "HEAD")
	if !strings.Contains(tree, "120000 blob ") || !strings.Contains(tree, "\tprojects/linked\n") ||
		strings.Contains(tree, "160000 commit ") || strings.Contains(tree, "projects/linked/") {
		t.Fatalf("linked project must be one mode 120000 blob without descendants:\n%s", tree)
	}
	target, err := dirlink.ReadTarget(f.link)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.git(f.repo, "show", "HEAD:projects/linked"); got != target {
		t.Fatalf("link blob = %q, want %q", got, target)
	}
	if got, err := os.ReadFile(filepath.Join(f.external, "external-only.txt")); err != nil || string(got) != "outside the camp\n" {
		t.Fatalf("external source changed: %q, %v", got, err)
	}
}
