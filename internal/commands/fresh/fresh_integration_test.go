//go:build integration

package fresh

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// run executes a command and fails the test on error.
func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\nOutput: %s", name, args, err, output)
	}
	return string(output)
}

// runWithEnv executes a command with custom environment in a directory.
func runWithEnv(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\nOutput: %s", name, args, err, output)
	}
	return string(output)
}

// setupCampaignWithSubmodule creates a campaign repo with a submodule
// under projects/ that has a bare remote origin. Returns (campaignDir, bareRemoteDir).
func setupCampaignWithSubmodule(t *testing.T) (string, string) {
	t.Helper()

	// Create bare remote for the submodule
	bareDir := t.TempDir()
	run(t, "git", "init", "--bare", bareDir)

	// Clone bare to create initial content, then push
	cloneDir := t.TempDir()
	run(t, "git", "clone", bareDir, cloneDir)
	run(t, "git", "-C", cloneDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", cloneDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(cloneDir, "README.md"), []byte("# Test Project"), 0644)
	run(t, "git", "-C", cloneDir, "add", ".")
	run(t, "git", "-C", cloneDir, "commit", "-m", "Initial commit")
	run(t, "git", "-C", cloneDir, "push", "origin", "main")

	// Create campaign repo
	campDir := t.TempDir()
	run(t, "git", "init", campDir)
	run(t, "git", "-C", campDir, "config", "user.email", "test@test.com")
	run(t, "git", "-C", campDir, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(campDir, "README.md"), []byte("# Campaign"), 0644)
	run(t, "git", "-C", campDir, "add", ".")
	run(t, "git", "-C", campDir, "commit", "-m", "Initial campaign commit")

	// Add submodule under projects/
	runWithEnv(t, campDir, []string{"GIT_ALLOW_PROTOCOL=file"},
		"git", "submodule", "add", bareDir, "projects/test-project")
	run(t, "git", "-C", campDir, "commit", "-m", "Add submodule")

	return campDir, bareDir
}

func TestIntegration_ExecuteFresh_CreatesAndPushesNewBranch(t *testing.T) {
	campDir, _ := setupCampaignWithSubmodule(t)
	subDir := filepath.Join(campDir, "projects", "test-project")

	err := executeFresh(context.Background(), "test-project", subDir, freshOptions{
		branch:      "feat/new-work",
		prune:       true,
		pruneRemote: true,
		push:        true,
	})
	if err != nil {
		t.Fatalf("executeFresh() error = %v", err)
	}

	current := strings.TrimSpace(run(t, "git", "-C", subDir, "rev-parse", "--abbrev-ref", "HEAD"))
	if current != "feat/new-work" {
		t.Fatalf("current branch = %q, want %q", current, "feat/new-work")
	}

	upstream := strings.TrimSpace(run(t, "git", "-C", subDir, "rev-parse", "--abbrev-ref", "@{upstream}"))
	if upstream != "origin/feat/new-work" {
		t.Fatalf("upstream = %q, want %q", upstream, "origin/feat/new-work")
	}
}

func TestIntegration_ExecuteFresh_DoesNotPushExistingBranch(t *testing.T) {
	campDir, _ := setupCampaignWithSubmodule(t)
	subDir := filepath.Join(campDir, "projects", "test-project")

	run(t, "git", "-C", subDir, "checkout", "-b", "develop")
	if err := os.WriteFile(filepath.Join(subDir, "develop.txt"), []byte("develop"), 0o644); err != nil {
		t.Fatalf("failed to write develop.txt: %v", err)
	}
	run(t, "git", "-C", subDir, "add", ".")
	run(t, "git", "-C", subDir, "commit", "-m", "Develop work")
	run(t, "git", "-C", subDir, "checkout", "main")

	err := executeFresh(context.Background(), "test-project", subDir, freshOptions{
		branch:      "develop",
		prune:       true,
		pruneRemote: true,
		push:        true,
	})
	if err != nil {
		t.Fatalf("executeFresh() error = %v", err)
	}

	current := strings.TrimSpace(run(t, "git", "-C", subDir, "rev-parse", "--abbrev-ref", "HEAD"))
	if current != "main" {
		t.Fatalf("current branch = %q, want %q", current, "main")
	}

	cmd := exec.Command("git", "-C", subDir, "rev-parse", "--abbrev-ref", "develop@{upstream}")
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected existing branch to remain without upstream, got %s", strings.TrimSpace(string(output)))
	}
}
