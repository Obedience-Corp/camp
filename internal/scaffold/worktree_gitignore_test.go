package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootGitignoreWorktreeRule(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{name: "default when empty", path: "", want: "/projects/worktrees/"},
		{name: "anchors configured path", path: "projects/worktrees/", want: "/projects/worktrees/"},
		{name: "trims surrounding slashes", path: "/custom/wt/", want: "/custom/wt/"},
		{name: "trims whitespace", path: "  projects/worktrees  ", want: "/projects/worktrees/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rootGitignoreWorktreeRule(tc.path); got != tc.want {
				t.Errorf("rootGitignoreWorktreeRule(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestGitignoreIgnoresWorktrees(t *testing.T) {
	cases := []struct {
		name    string
		content string
		path    string
		want    bool
	}{
		{name: "anchored directory rule", content: "/projects/worktrees/\n", path: "projects/worktrees/", want: true},
		{name: "unanchored directory rule", content: "projects/worktrees/\n", path: "projects/worktrees/", want: true},
		{name: "bare directory rule", content: "worktrees/\n", path: "projects/worktrees/", want: true},
		{name: "bare directory no slash", content: "worktrees\n", path: "projects/worktrees/", want: true},
		{name: "absent", content: "state.yaml\n*.db\n", path: "projects/worktrees/", want: false},
		{name: "commented out is not a rule", content: "# worktrees/\n", path: "projects/worktrees/", want: false},
		{name: "default path when empty", content: "/projects/worktrees/\n", path: "", want: true},
		{name: "custom path matched", content: "/custom/wt/\n", path: "custom/wt/", want: true},
		{name: "custom path absent", content: "worktrees/\n", path: "custom/wt/", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := gitignoreIgnoresWorktrees(tc.content, tc.path); got != tc.want {
				t.Errorf("gitignoreIgnoresWorktrees(%q, %q) = %v, want %v", tc.content, tc.path, got, tc.want)
			}
		})
	}
}

func TestEnsureRootGitignoreWorktreesCreatesFile(t *testing.T) {
	dir := t.TempDir()

	created, modified, err := ensureRootGitignoreWorktrees(dir, "projects/worktrees/")
	if err != nil {
		t.Fatalf("ensureRootGitignoreWorktrees() error = %v", err)
	}
	if !created || modified {
		t.Fatalf("first call: got created=%v modified=%v, want created=true modified=false", created, modified)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !gitignoreIgnoresWorktrees(string(content), "projects/worktrees/") {
		t.Fatalf("root .gitignore missing worktrees rule:\n%s", content)
	}
	if !strings.Contains(string(content), "machine-local") {
		t.Fatalf("root .gitignore missing explanatory comment:\n%s", content)
	}

	created, modified, err = ensureRootGitignoreWorktrees(dir, "projects/worktrees/")
	if err != nil {
		t.Fatalf("second ensureRootGitignoreWorktrees() error = %v", err)
	}
	if created || modified {
		t.Fatalf("second call should be a no-op: got created=%v modified=%v", created, modified)
	}
}

func TestEnsureRootGitignoreWorktreesAppendsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	gitignorePath := filepath.Join(dir, ".gitignore")
	existing := ".env\n*.db\n"
	if err := os.WriteFile(gitignorePath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	created, modified, err := ensureRootGitignoreWorktrees(dir, "projects/worktrees/")
	if err != nil {
		t.Fatalf("ensureRootGitignoreWorktrees() error = %v", err)
	}
	if created || !modified {
		t.Fatalf("got created=%v modified=%v, want created=false modified=true", created, modified)
	}

	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), existing) {
		t.Fatalf("existing content not preserved:\n%s", content)
	}
	if !gitignoreIgnoresWorktrees(string(content), "projects/worktrees/") {
		t.Fatalf("worktrees rule not appended:\n%s", content)
	}
}

func TestEnsureRootGitignoreWorktreesHonorsExistingBareRule(t *testing.T) {
	dir := t.TempDir()
	gitignorePath := filepath.Join(dir, ".gitignore")
	existing := "# Worktrees directory\nworktrees/\n"
	if err := os.WriteFile(gitignorePath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	created, modified, err := ensureRootGitignoreWorktrees(dir, "projects/worktrees/")
	if err != nil {
		t.Fatalf("ensureRootGitignoreWorktrees() error = %v", err)
	}
	if created || modified {
		t.Fatalf("existing bare rule should be honored: got created=%v modified=%v", created, modified)
	}

	content, err := os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != existing {
		t.Fatalf("file should be unchanged:\ngot:\n%s\nwant:\n%s", content, existing)
	}
}
