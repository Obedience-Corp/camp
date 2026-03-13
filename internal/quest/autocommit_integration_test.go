package quest

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/git"
)

// initGitRepo initialises a bare git repo in dir so that git operations work.
// It sets a local user identity to avoid "Author identity unknown" errors in CI.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", args[0], err, out)
		}
	}
}

// gitLsTree returns the list of all file paths in the HEAD commit tree.
func gitLsTree(t *testing.T, repoDir string) []string {
	t.Helper()
	cmd := exec.Command("git", "-C", repoDir, "ls-tree", "-r", "HEAD", "--name-only")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-tree: %v", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

// TestAutoCommitLifecycle verifies that after completing a quest and running
// the equivalent of autoCommitQuest, the old quest directory path is absent
// from the committed tree and the new dungeon path is present.
func TestAutoCommitLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// Initialise a real git repo so we can commit.
	initGitRepo(t, root)

	// Bootstrap quest scaffold.
	if _, err := EnsureScaffold(ctx, root); err != nil {
		t.Fatalf("EnsureScaffold() error = %v", err)
	}

	// Stage and commit the scaffold so it appears in HEAD before we create a quest.
	if err := git.StageAll(ctx, root); err != nil {
		t.Fatalf("stage scaffold: %v", err)
	}
	if err := git.Commit(ctx, root, &git.CommitOptions{Message: "chore: init quest scaffold"}); err != nil {
		t.Fatalf("commit scaffold: %v", err)
	}

	svc := NewService(root)

	// Create a quest.
	result, err := svc.Create(ctx, "Lifecycle Quest", "Test the commit lifecycle", "", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	q := result.Quest
	oldQuestDir := filepath.Dir(q.Path)

	// Stage and commit the new quest so it is tracked.
	if err := git.StageFiles(ctx, root, result.Files...); err != nil {
		t.Fatalf("stage quest create: %v", err)
	}
	if err := git.Commit(ctx, root, &git.CommitOptions{Message: "feat: create lifecycle quest"}); err != nil {
		t.Fatalf("commit quest create: %v", err)
	}

	// Confirm the quest is visible in HEAD before completion.
	treeBeforeComplete := gitLsTree(t, root)
	oldRel, _ := filepath.Rel(root, q.Path)
	if !treeContainsPrefix(treeBeforeComplete, filepath.Dir(oldRel)) {
		t.Fatalf("expected quest path %q to be in tree before completion; tree: %v", oldRel, treeBeforeComplete)
	}

	// Complete the quest — this moves oldQuestDir → dungeon/completed/<slug>.
	completed, err := svc.Complete(ctx, q.ID)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	newQuestDir := filepath.Dir(completed.Quest.Path)

	// Mirror autoCommitQuest: remove old (pre-staged) path from index, then
	// add the new dungeon directory and any other mutated files.
	oldRelDir, _ := filepath.Rel(root, oldQuestDir)
	if err := git.RemoveCached(ctx, root, oldRelDir); err != nil {
		t.Fatalf("RemoveCached() error = %v", err)
	}
	if err := git.StageFiles(ctx, root, completed.Files...); err != nil {
		t.Fatalf("stage quest complete: %v", err)
	}
	if err := git.Commit(ctx, root, &git.CommitOptions{Message: "feat: complete lifecycle quest"}); err != nil {
		t.Fatalf("commit quest complete: %v", err)
	}

	// Assert: old path is NOT in HEAD tree.
	treeAfterComplete := gitLsTree(t, root)
	if treeContainsPrefix(treeAfterComplete, oldRelDir) {
		t.Errorf("old quest path %q should NOT be in tree after completion, but it is; tree: %v", oldRelDir, treeAfterComplete)
	}

	// Assert: new dungeon path IS in HEAD tree.
	newRelDir, _ := filepath.Rel(root, newQuestDir)
	if !treeContainsPrefix(treeAfterComplete, newRelDir) {
		t.Errorf("new quest path %q should be in tree after completion, but it is not; tree: %v", newRelDir, treeAfterComplete)
	}

	// Sanity: quest is in the completed dungeon bucket.
	expectedDungeonParent := DungeonStatusDir(root, StatusCompleted)
	if !strings.HasPrefix(newQuestDir, expectedDungeonParent) {
		t.Errorf("completed quest dir %q should be under dungeon/completed, got parent %q", newQuestDir, expectedDungeonParent)
	}

}

// treeContainsPrefix reports whether any path in tree has the given prefix component.
func treeContainsPrefix(tree []string, prefix string) bool {
	for _, p := range tree {
		if strings.HasPrefix(p, prefix+"/") || p == prefix {
			return true
		}
	}
	return false
}
