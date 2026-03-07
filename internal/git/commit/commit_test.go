package commit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntent_MissingCampaignInfo(t *testing.T) {
	ctx := context.Background()

	// Test with empty campaign root
	result := Intent(ctx, IntentOptions{
		Options: Options{
			CampaignRoot: "",
			CampaignID:   "test-id",
		},
		Action:      IntentCreate,
		IntentTitle: "Test Intent",
	})

	if result.Committed {
		t.Error("expected Committed to be false when CampaignRoot is empty")
	}
	if result.Message != "" {
		t.Errorf("expected empty message when CampaignRoot is empty, got: %s", result.Message)
	}

	// Test with empty campaign ID
	result = Intent(ctx, IntentOptions{
		Options: Options{
			CampaignRoot: "/some/path",
			CampaignID:   "",
		},
		Action:      IntentCreate,
		IntentTitle: "Test Intent",
	})

	if result.Committed {
		t.Error("expected Committed to be false when CampaignID is empty")
	}
}

func TestIntent_TruncatesCampaignID(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := context.Background()

	longID := "abcdefghijklmnopqrstuvwxyz123456"
	result := Intent(ctx, IntentOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   longID,
		},
		Action:      IntentCreate,
		IntentTitle: "Test Intent",
	})

	if !result.Committed {
		t.Errorf("expected commit to succeed, got message: %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "log", "-1", "--oneline").Output()
	if err != nil {
		t.Fatalf("failed to get git log: %v", err)
	}

	commitMsg := string(out)
	expectedPrefix := "[OBEY-CAMPAIGN-abcdefgh]"
	if !strings.Contains(commitMsg, expectedPrefix) {
		t.Errorf("commit message should contain truncated ID prefix %q, got: %s", expectedPrefix, commitMsg)
	}
}

func TestIntent_NoChanges(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	testFile := filepath.Join(tmpDir, "initial.txt")
	if err := os.WriteFile(testFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to add file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "initial").Run(); err != nil {
		t.Fatalf("failed to initial commit: %v", err)
	}

	ctx := context.Background()

	result := Intent(ctx, IntentOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		Action:      IntentMove,
		IntentTitle: "Test Intent",
	})

	if result.Committed {
		t.Error("expected Committed to be false when there are no changes")
	}
	if result.Message != "(no changes to commit)" {
		t.Errorf("expected 'no changes to commit' message, got: %s", result.Message)
	}
}

func TestIntent_WithDescription(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := context.Background()

	result := Intent(ctx, IntentOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		Action:      IntentMove,
		IntentTitle: "Test Intent",
		Description: "Moved to active status",
	})

	if !result.Committed {
		t.Errorf("expected commit to succeed, got message: %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("failed to get git log: %v", err)
	}

	commitMsg := string(out)
	if !strings.Contains(commitMsg, "Moved to active status") {
		t.Errorf("commit message should contain description, got: %s", commitMsg)
	}
}

func TestIntent_SelectiveStaging(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	initialFile := filepath.Join(tmpDir, "initial.txt")
	if err := os.WriteFile(initialFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to add initial file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "initial").Run(); err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}

	intended := filepath.Join(tmpDir, "workflow", "intents", "inbox", "intent.md")
	if err := os.MkdirAll(filepath.Dir(intended), 0755); err != nil {
		t.Fatalf("failed to create intended parent: %v", err)
	}
	if err := os.WriteFile(intended, []byte("intent"), 0644); err != nil {
		t.Fatalf("failed to create intended file: %v", err)
	}

	unrelated := filepath.Join(tmpDir, "unrelated.txt")
	if err := os.WriteFile(unrelated, []byte("wip"), 0644); err != nil {
		t.Fatalf("failed to create unrelated file: %v", err)
	}

	ctx := context.Background()
	result := Intent(ctx, IntentOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
			Files:        []string{filepath.Join("workflow", "intents", "inbox", "intent.md")},
		},
		Action:      IntentCreate,
		IntentTitle: "Selective intent",
	})

	if !result.Committed {
		t.Fatalf("expected commit to succeed, got message: %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to get committed files: %v", err)
	}
	committed := string(out)
	if !strings.Contains(committed, "workflow/intents/inbox/intent.md") {
		t.Fatalf("expected intended file in commit, got: %s", committed)
	}
	if strings.Contains(committed, "unrelated.txt") {
		t.Fatalf("unrelated file should not be committed: %s", committed)
	}
}

func TestIntentAction_Values(t *testing.T) {
	tests := []struct {
		action   IntentAction
		expected string
	}{
		{IntentCreate, "Create"},
		{IntentMove, "Move"},
		{IntentArchive, "Archive"},
		{IntentDelete, "Delete"},
		{IntentGather, "Gather"},
		{IntentPromote, "Promote"},
	}

	for _, tt := range tests {
		if string(tt.action) != tt.expected {
			t.Errorf("IntentAction %v expected %q, got %q", tt.action, tt.expected, string(tt.action))
		}
	}
}

func TestCrawl(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	testFile := filepath.Join(tmpDir, "crawl.txt")
	if err := os.WriteFile(testFile, []byte("crawl data"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := context.Background()

	result := Crawl(ctx, CrawlOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		Description: "Moved 3 items to archive",
	})

	if !result.Committed {
		t.Errorf("expected commit to succeed, got message: %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("failed to get git log: %v", err)
	}

	commitMsg := string(out)
	if !strings.Contains(commitMsg, "Crawl: dungeon crawl completed") {
		t.Errorf("commit message should contain crawl subject, got: %s", commitMsg)
	}
	if !strings.Contains(commitMsg, "Moved 3 items to archive") {
		t.Errorf("commit message should contain description, got: %s", commitMsg)
	}
}

func TestCrawl_SelectiveStaging(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	// Create initial commit so we have a baseline
	initialFile := filepath.Join(tmpDir, "initial.txt")
	if err := os.WriteFile(initialFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to add initial file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "initial").Run(); err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}

	// Create a dungeon directory with a crawl file (intended change)
	dungeonDir := filepath.Join(tmpDir, "dungeon")
	if err := os.MkdirAll(dungeonDir, 0755); err != nil {
		t.Fatalf("failed to create dungeon dir: %v", err)
	}
	crawlFile := filepath.Join(dungeonDir, "moved-item.txt")
	if err := os.WriteFile(crawlFile, []byte("moved"), 0644); err != nil {
		t.Fatalf("failed to create crawl file: %v", err)
	}

	// Create an unrelated file (should NOT be committed)
	unrelatedFile := filepath.Join(tmpDir, "unrelated-change.txt")
	if err := os.WriteFile(unrelatedFile, []byte("unrelated"), 0644); err != nil {
		t.Fatalf("failed to create unrelated file: %v", err)
	}

	ctx := context.Background()

	// Commit with Files set to only the dungeon directory
	result := Crawl(ctx, CrawlOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		Description: "Moved items to archive",
		Files:       []string{"dungeon"},
	})

	if !result.Committed {
		t.Errorf("expected commit to succeed, got message: %s", result.Message)
	}

	// Verify: the committed files should only include dungeon/moved-item.txt
	out, err := exec.Command("git", "-C", tmpDir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to get committed files: %v", err)
	}

	committedFiles := strings.TrimSpace(string(out))
	if !strings.Contains(committedFiles, "dungeon/moved-item.txt") {
		t.Errorf("expected dungeon/moved-item.txt in commit, got: %s", committedFiles)
	}
	if strings.Contains(committedFiles, "unrelated-change.txt") {
		t.Errorf("unrelated-change.txt should NOT be in commit, got: %s", committedFiles)
	}

	// Verify: unrelated file should still be untracked
	statusOut, err := exec.Command("git", "-C", tmpDir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("failed to get git status: %v", err)
	}
	status := string(statusOut)
	if !strings.Contains(status, "unrelated-change.txt") {
		t.Errorf("unrelated-change.txt should still be untracked, status: %s", status)
	}
}

func TestCrawl_SelectiveStaging_FromRoot(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	// Create initial commit
	initialFile := filepath.Join(tmpDir, "initial.txt")
	if err := os.WriteFile(initialFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to add initial file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "initial").Run(); err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}

	// Simulate crawl from campaign root: relCwd would be ".", relDungeon is "dungeon"
	dungeonDir := filepath.Join(tmpDir, "dungeon")
	if err := os.MkdirAll(dungeonDir, 0755); err != nil {
		t.Fatalf("failed to create dungeon dir: %v", err)
	}
	crawlFile := filepath.Join(dungeonDir, "archived-item.txt")
	if err := os.WriteFile(crawlFile, []byte("archived"), 0644); err != nil {
		t.Fatalf("failed to create crawl file: %v", err)
	}

	// Unrelated file at root — must NOT be committed
	unrelatedFile := filepath.Join(tmpDir, "wip-notes.txt")
	if err := os.WriteFile(unrelatedFile, []byte("work in progress"), 0644); err != nil {
		t.Fatalf("failed to create unrelated file: %v", err)
	}

	ctx := context.Background()

	// Files = ["dungeon"] simulates the fixed behavior (no "." in the list)
	result := Crawl(ctx, CrawlOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		Description: "Crawl from root",
		Files:       []string{"dungeon"},
	})

	if !result.Committed {
		t.Errorf("expected commit to succeed, got message: %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to get committed files: %v", err)
	}

	committedFiles := strings.TrimSpace(string(out))
	if !strings.Contains(committedFiles, "dungeon/archived-item.txt") {
		t.Errorf("expected dungeon/archived-item.txt in commit, got: %s", committedFiles)
	}
	if strings.Contains(committedFiles, "wip-notes.txt") {
		t.Errorf("wip-notes.txt should NOT be in commit (root staging bug), got: %s", committedFiles)
	}
}

func TestCrawl_SelectiveStaging_IgnoresPreStaged(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	// Create initial commit
	initialFile := filepath.Join(tmpDir, "initial.txt")
	if err := os.WriteFile(initialFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to add initial file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "initial").Run(); err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}

	// Create dungeon file (intended change)
	dungeonDir := filepath.Join(tmpDir, "dungeon")
	if err := os.MkdirAll(dungeonDir, 0755); err != nil {
		t.Fatalf("failed to create dungeon dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dungeonDir, "crawled.txt"), []byte("crawled"), 0644); err != nil {
		t.Fatalf("failed to create crawl file: %v", err)
	}

	// Pre-stage an unrelated file — this is the key scenario
	unrelatedFile := filepath.Join(tmpDir, "unrelated.txt")
	if err := os.WriteFile(unrelatedFile, []byte("unrelated"), 0644); err != nil {
		t.Fatalf("failed to create unrelated file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", "unrelated.txt").Run(); err != nil {
		t.Fatalf("failed to pre-stage unrelated file: %v", err)
	}

	ctx := context.Background()

	result := Crawl(ctx, CrawlOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		Description: "Selective crawl with dirty index",
		Files:       []string{"dungeon"},
	})

	if !result.Committed {
		t.Fatalf("expected commit to succeed, got message: %s", result.Message)
	}

	// Verify only dungeon files were committed, not the pre-staged unrelated.txt
	out, err := exec.Command("git", "-C", tmpDir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to get committed files: %v", err)
	}

	committedFiles := strings.TrimSpace(string(out))
	if !strings.Contains(committedFiles, "dungeon/crawled.txt") {
		t.Errorf("expected dungeon/crawled.txt in commit, got: %s", committedFiles)
	}
	if strings.Contains(committedFiles, "unrelated.txt") {
		t.Errorf("pre-staged unrelated.txt should NOT be in commit, got: %s", committedFiles)
	}

	// Verify unrelated.txt is still staged (not lost)
	statusOut, err := exec.Command("git", "-C", tmpDir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("failed to get git status: %v", err)
	}
	status := string(statusOut)
	if !strings.Contains(status, "unrelated.txt") {
		t.Errorf("unrelated.txt should still be staged, status: %s", status)
	}
}

func TestDoCommit_EmptyFiles_StagesAll(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	// Create two files — both should be committed when Files is empty
	file1 := filepath.Join(tmpDir, "file1.txt")
	if err := os.WriteFile(file1, []byte("one"), 0644); err != nil {
		t.Fatalf("failed to create file1: %v", err)
	}
	file2 := filepath.Join(tmpDir, "file2.txt")
	if err := os.WriteFile(file2, []byte("two"), 0644); err != nil {
		t.Fatalf("failed to create file2: %v", err)
	}

	ctx := context.Background()

	// Crawl with no Files (nil) and SelectiveOnly=false should stage everything (legacy)
	result := Crawl(ctx, CrawlOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		Description: "Full staging test",
	})

	if !result.Committed {
		t.Errorf("expected commit to succeed, got message: %s", result.Message)
	}

	// Use --root for the initial commit (no parent to diff against)
	out, err := exec.Command("git", "-C", tmpDir, "diff-tree", "--root", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to get committed files: %v", err)
	}

	committedFiles := strings.TrimSpace(string(out))
	if !strings.Contains(committedFiles, "file1.txt") {
		t.Errorf("expected file1.txt in commit, got: %s", committedFiles)
	}
	if !strings.Contains(committedFiles, "file2.txt") {
		t.Errorf("expected file2.txt in commit, got: %s", committedFiles)
	}
}

func TestSelectiveOnly_EmptyFiles_NoCommit(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	// Create an initial commit so HEAD exists
	seedFile := filepath.Join(tmpDir, "seed.txt")
	if err := os.WriteFile(seedFile, []byte("seed"), 0644); err != nil {
		t.Fatalf("failed to create seed file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to stage seed: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "seed").Run(); err != nil {
		t.Fatalf("failed to commit seed: %v", err)
	}

	// Create an unrelated dirty file that should NOT be committed
	dirtyFile := filepath.Join(tmpDir, "dirty.txt")
	if err := os.WriteFile(dirtyFile, []byte("wip"), 0644); err != nil {
		t.Fatalf("failed to create dirty file: %v", err)
	}

	ctx := context.Background()

	// Repair with SelectiveOnly=true and empty Files should NOT commit
	result := Repair(ctx, RepairOptions{
		Options: Options{
			CampaignRoot:  tmpDir,
			CampaignID:    "test1234",
			SelectiveOnly: true,
		},
		Description: "Repair with no file targets",
	})

	if result.Committed {
		t.Errorf("expected no commit when SelectiveOnly is true with empty Files")
	}

	// Verify the dirty file was NOT committed
	out, err := exec.Command("git", "-C", tmpDir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("failed to get git status: %v", err)
	}
	if !strings.Contains(string(out), "dirty.txt") {
		t.Errorf("dirty.txt should still be uncommitted, git status: %s", string(out))
	}
}

func TestRepair(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	testFile := filepath.Join(tmpDir, "repair.txt")
	if err := os.WriteFile(testFile, []byte("repair data"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := context.Background()

	result := Repair(ctx, RepairOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		Description: "Added missing directories",
	})

	if !result.Committed {
		t.Errorf("expected commit to succeed, got message: %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("failed to get git log: %v", err)
	}

	commitMsg := string(out)
	if !strings.Contains(commitMsg, "Repair: campaign repair") {
		t.Errorf("commit message should contain repair subject, got: %s", commitMsg)
	}
}

func TestProject(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	testFile := filepath.Join(tmpDir, "project.txt")
	if err := os.WriteFile(testFile, []byte("project data"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := context.Background()

	result := Project(ctx, ProjectOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		Action:      ProjectAdd,
		ProjectName: "my-service",
	})

	if !result.Committed {
		t.Errorf("expected commit to succeed, got message: %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("failed to get git log: %v", err)
	}

	commitMsg := string(out)
	if !strings.Contains(commitMsg, "Add: my-service") {
		t.Errorf("commit message should contain project action and name, got: %s", commitMsg)
	}
}

func TestProject_SelectiveStaging(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	initialFile := filepath.Join(tmpDir, "initial.txt")
	if err := os.WriteFile(initialFile, []byte("initial"), 0644); err != nil {
		t.Fatalf("failed to create initial file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to add initial file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "initial").Run(); err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}

	gitmodules := filepath.Join(tmpDir, ".gitmodules")
	if err := os.WriteFile(gitmodules, []byte("[submodule \"projects/my-service\"]\n"), 0644); err != nil {
		t.Fatalf("failed to create .gitmodules: %v", err)
	}

	projectFile := filepath.Join(tmpDir, "projects", "my-service", "README.md")
	if err := os.MkdirAll(filepath.Dir(projectFile), 0755); err != nil {
		t.Fatalf("failed to create project dir: %v", err)
	}
	if err := os.WriteFile(projectFile, []byte("# my-service"), 0644); err != nil {
		t.Fatalf("failed to create project file: %v", err)
	}

	unrelated := filepath.Join(tmpDir, "notes.txt")
	if err := os.WriteFile(unrelated, []byte("scratch"), 0644); err != nil {
		t.Fatalf("failed to create unrelated file: %v", err)
	}

	ctx := context.Background()
	result := Project(ctx, ProjectOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
			Files:        []string{".gitmodules", filepath.Join("projects", "my-service")},
		},
		Action:      ProjectAdd,
		ProjectName: "my-service",
	})

	if !result.Committed {
		t.Fatalf("expected commit to succeed, got message: %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to get committed files: %v", err)
	}
	committed := string(out)
	if !strings.Contains(committed, ".gitmodules") {
		t.Fatalf("expected .gitmodules in commit, got: %s", committed)
	}
	if !strings.Contains(committed, "projects/my-service/README.md") {
		t.Fatalf("expected project file in commit, got: %s", committed)
	}
	if strings.Contains(committed, "notes.txt") {
		t.Fatalf("unrelated file should not be committed: %s", committed)
	}
}

func TestProjectAction_Values(t *testing.T) {
	tests := []struct {
		action   ProjectAction
		expected string
	}{
		{ProjectAdd, "Add"},
		{ProjectNew, "New"},
		{ProjectRemove, "Remove"},
	}

	for _, tt := range tests {
		if string(tt.action) != tt.expected {
			t.Errorf("ProjectAction %v expected %q, got %q", tt.action, tt.expected, string(tt.action))
		}
	}
}

func TestCrawl_PreStagedOnly_CommitsDeletedPaths(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	// Create and commit a file
	sourceFile := filepath.Join(tmpDir, "source.txt")
	if err := os.WriteFile(sourceFile, []byte("source content"), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to add source file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "initial").Run(); err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}

	// Delete the file from disk and stage the deletion with git add -u
	if err := os.Remove(sourceFile); err != nil {
		t.Fatalf("failed to delete source file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", "-u", "source.txt").Run(); err != nil {
		t.Fatalf("failed to stage deletion: %v", err)
	}

	ctx := context.Background()

	// Crawl with no Files, only PreStaged — should commit the deletion
	result := Crawl(ctx, CrawlOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
			PreStaged:    []string{"source.txt"},
		},
		Description: "PreStaged-only deletion",
	})

	if !result.Committed {
		t.Fatalf("expected commit to succeed, got message: %s", result.Message)
	}

	// Verify source.txt was deleted in the commit
	out, err := exec.Command("git", "-C", tmpDir, "diff-tree", "--no-commit-id", "--name-status", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to get committed files: %v", err)
	}

	committedFiles := string(out)
	if !strings.Contains(committedFiles, "D\tsource.txt") {
		t.Errorf("expected source.txt deletion in commit, got: %s", committedFiles)
	}
}

func TestCrawl_FilesAndPreStaged_CombinedScope(t *testing.T) {
	tmpDir := t.TempDir()

	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	// Create and commit two files
	sourceFile := filepath.Join(tmpDir, "source.txt")
	if err := os.WriteFile(sourceFile, []byte("source"), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}
	untouchedFile := filepath.Join(tmpDir, "untouched.txt")
	if err := os.WriteFile(untouchedFile, []byte("untouched"), 0644); err != nil {
		t.Fatalf("failed to create untouched file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to add files: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "initial").Run(); err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}

	// Create destination file (simulates triage move target)
	dungeonDir := filepath.Join(tmpDir, "dungeon")
	if err := os.MkdirAll(dungeonDir, 0755); err != nil {
		t.Fatalf("failed to create dungeon dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dungeonDir, "moved.txt"), []byte("moved"), 0644); err != nil {
		t.Fatalf("failed to create moved file: %v", err)
	}

	// Delete source and stage deletion (simulates triage source removal)
	if err := os.Remove(sourceFile); err != nil {
		t.Fatalf("failed to delete source file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", "-u", "source.txt").Run(); err != nil {
		t.Fatalf("failed to stage deletion: %v", err)
	}

	ctx := context.Background()

	// Crawl with Files (destination) + PreStaged (source deletion)
	result := Crawl(ctx, CrawlOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
			PreStaged:    []string{"source.txt"},
		},
		Description: "Files + PreStaged combined",
		Files:       []string{"dungeon"},
	})

	if !result.Committed {
		t.Fatalf("expected commit to succeed, got message: %s", result.Message)
	}

	// Verify both the addition and deletion appear in the commit
	out, err := exec.Command("git", "-C", tmpDir, "diff-tree", "--no-commit-id", "--name-status", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to get committed files: %v", err)
	}

	committedFiles := string(out)
	if !strings.Contains(committedFiles, "A\tdungeon/moved.txt") {
		t.Errorf("expected dungeon/moved.txt addition in commit, got: %s", committedFiles)
	}
	if !strings.Contains(committedFiles, "D\tsource.txt") {
		t.Errorf("expected source.txt deletion in commit, got: %s", committedFiles)
	}
	if strings.Contains(committedFiles, "untouched.txt") {
		t.Errorf("untouched.txt should NOT appear in commit, got: %s", committedFiles)
	}
}
