//go:build container_fs

// The initial-commit tests run git init and git commit against a real
// repository, so they build only under the container_fs tag and execute in
// the integration harness's pooled container, never on the host.

package initcmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/defercommit"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newInitFixture(t *testing.T, name string) string {
	t.Helper()
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv(defercommit.EnvNoDefer, "1")
	dir := filepath.Join(tmpDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func runInitFlow(t *testing.T, dir, name string, noGit bool) string {
	t.Helper()
	var out bytes.Buffer
	err := RunFlow(t.Context(), Params{
		Dir:         dir,
		Name:        name,
		TypeStr:     "product",
		Description: "d",
		Mission:     "m",
		NoRegister:  true,
		NoGit:       noGit,
		NoSkills:    true,
	}, Writers{HumanOut: &out, ErrOut: &out}, false)
	t.Logf("init output:\n%s", out.String())
	if err != nil {
		t.Fatalf("RunFlow() error = %v", err)
	}
	return out.String()
}

// A new workspace starts with its scaffold committed: one commit, the camp
// commit-message contract in the subject, and a clean tree afterwards.
func TestRunFlow_FreshInitCreatesInitialCommit(t *testing.T) {
	dir := newInitFixture(t, "fresh-camp")
	out := runInitFlow(t, dir, "fresh-camp", false)

	if !strings.Contains(out, "Initial commit created") {
		t.Fatalf("init did not report the initial commit")
	}
	if got := gitOut(t, dir, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("commit count = %s, want 1", got)
	}
	subject := gitOut(t, dir, "log", "-1", "--format=%s")
	if !strings.HasPrefix(subject, "[fresh-camp:") || !strings.HasSuffix(subject, "] Init: scaffold camp workspace") {
		t.Fatalf("subject = %q", subject)
	}
	body := gitOut(t, dir, "log", "-1", "--format=%b")
	t.Logf("commit body:\n%s", body)
	if !strings.Contains(body, "Files created:") || !strings.Contains(body, ".campaign/campaign.yaml") {
		t.Fatalf("body does not describe the scaffold")
	}
	if status := gitOut(t, dir, "status", "--porcelain"); status != "" {
		t.Fatalf("scaffold left uncommitted files:\n%s", status)
	}
	tracked := gitOut(t, dir, "ls-files")
	t.Logf("tracked:\n%s", tracked)
	for _, want := range []string{".campaign/campaign.yaml", "AGENTS.md", ".gitignore"} {
		if !strings.Contains(tracked, want) {
			t.Errorf("%s not in the initial commit", want)
		}
	}
}

// --no-git means no repository, so there is nothing to commit and init must
// not create one on the side.
func TestRunFlow_NoGitSkipsInitialCommit(t *testing.T) {
	dir := newInitFixture(t, "no-git-camp")
	out := runInitFlow(t, dir, "no-git-camp", true)

	if strings.Contains(out, "Initial commit") {
		t.Fatalf("init mentioned a commit with --no-git")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git present after --no-git (stat err = %v)", err)
	}
}

// Init inside an existing repository stages only the scaffold. A dirty
// unrelated file stays out of the first camp commit.
func TestRunFlow_InitInsideExistingRepoLeavesUnrelatedChangesAlone(t *testing.T) {
	dir := newInitFixture(t, "nested-camp")
	gitOut(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, dir, "add", "notes.txt")
	gitOut(t, dir, "commit", "-q", "-m", "user work")
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine, edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := RunFlow(t.Context(), Params{
		Dir:         dir,
		Name:        "nested-camp",
		TypeStr:     "product",
		Description: "d",
		Mission:     "m",
		Force:       true,
		NoRegister:  true,
		NoSkills:    true,
	}, Writers{HumanOut: &out, ErrOut: &out}, false)
	t.Logf("init output:\n%s", out.String())
	if err != nil {
		t.Fatalf("RunFlow() error = %v", err)
	}

	if got := gitOut(t, dir, "rev-list", "--count", "HEAD"); got != "2" {
		t.Fatalf("commit count = %s, want 2 (user commit + init)", got)
	}
	if changed := gitOut(t, dir, "show", "--name-only", "--format=", "HEAD"); strings.Contains(changed, "notes.txt") {
		t.Fatalf("init commit swept up the user's dirty file:\n%s", changed)
	}
	if status := gitOut(t, dir, "status", "--porcelain"); status != "M notes.txt" {
		t.Fatalf("expected only the user's edit to remain dirty, got:\n%s", status)
	}
}
