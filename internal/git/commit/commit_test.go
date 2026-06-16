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
	if !result.NoChanges {
		t.Error("expected NoChanges to be true when there are no changes")
	}
	if result.Err != nil {
		t.Errorf("expected Err to be nil when there are no changes, got: %v", result.Err)
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

func TestIntent_WithQuestTag(t *testing.T) {
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

	testFile := filepath.Join(tmpDir, "quest.txt")
	if err := os.WriteFile(testFile, []byte("quest content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	result := Intent(context.Background(), IntentOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "abcdef12",
			QuestID:      "qst_20260313_abc123",
		},
		Action:      IntentCreate,
		IntentTitle: "Quest tagged intent",
	})

	if !result.Committed {
		t.Fatalf("expected commit to succeed, got %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("failed to get git log: %v", err)
	}
	if !strings.Contains(string(out), "[OBEY-CAMPAIGN-abcdef12-qst_20260313_abc123]") {
		t.Fatalf("commit message missing quest tag: %s", string(out))
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

	intended := filepath.Join(tmpDir, ".campaign", "intents", "inbox", "intent.md")
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
			Files:        []string{filepath.Join(".campaign", "intents", "inbox", "intent.md")},
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
	if !strings.Contains(committed, ".campaign/intents/inbox/intent.md") {
		t.Fatalf("expected intended file in commit, got: %s", committed)
	}
	if strings.Contains(committed, "unrelated.txt") {
		t.Fatalf("unrelated file should not be committed: %s", committed)
	}
}

func TestStageAndCommit_PreStagedCommitsStagedBlobNotWorktree(t *testing.T) {
	repo := initTempIndexCommitRepo(t)
	writeCommitTestFile(t, repo, "f.txt", "v1\n")
	runCommitTestGit(t, repo, "add", "f.txt")
	runCommitTestGit(t, repo, "commit", "-m", "init")

	writeCommitTestFile(t, repo, "f.txt", "v2\n")
	runCommitTestGit(t, repo, "add", "f.txt")
	writeCommitTestFile(t, repo, "f.txt", "v3\n")

	err := stageAndCommit(context.Background(), Options{
		CampaignRoot:  repo,
		PreStaged:     []string{"f.txt"},
		SelectiveOnly: true,
	}, "commit staged snapshot")
	if err != nil {
		t.Fatalf("stageAndCommit() error = %v", err)
	}

	if got := runCommitTestGitOutput(t, repo, "show", "HEAD:f.txt"); got != "v2\n" {
		t.Fatalf("HEAD:f.txt = %q, want v2", got)
	}
	if got := readCommitTestFile(t, repo, "f.txt"); got != "v3\n" {
		t.Fatalf("worktree f.txt = %q, want v3", got)
	}
	if got := runCommitTestGitOutput(t, repo, "diff", "--cached", "--name-only"); strings.TrimSpace(got) != "" {
		t.Fatalf("cached diff should be empty after commit, got: %s", got)
	}
	if got := runCommitTestGitOutput(t, repo, "diff", "--", "f.txt"); !strings.Contains(got, "v3") {
		t.Fatalf("worktree v3 should remain unstaged after commit, diff: %s", got)
	}
}

func TestStageAndCommit_FilesCaptureAddTimeSnapshot(t *testing.T) {
	repo := initTempIndexCommitRepo(t)
	writeCommitTestFile(t, repo, "f.txt", "v1\n")
	runCommitTestGit(t, repo, "add", "f.txt")
	runCommitTestGit(t, repo, "commit", "-m", "init")

	writeCommitTestFile(t, repo, "f.txt", "v2\n")
	hookPath := filepath.Join(repo, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\nprintf 'v3\\n' > f.txt\n"), 0o755); err != nil {
		t.Fatalf("failed to write pre-commit hook: %v", err)
	}

	err := stageAndCommit(context.Background(), Options{
		CampaignRoot:  repo,
		Files:         []string{"f.txt"},
		SelectiveOnly: true,
	}, "commit add-time snapshot")
	if err != nil {
		t.Fatalf("stageAndCommit() error = %v", err)
	}

	if got := runCommitTestGitOutput(t, repo, "show", "HEAD:f.txt"); got != "v2\n" {
		t.Fatalf("HEAD:f.txt = %q, want add-time v2", got)
	}
	if got := readCommitTestFile(t, repo, "f.txt"); got != "v3\n" {
		t.Fatalf("worktree f.txt = %q, want hook-written v3", got)
	}
	if got := runCommitTestGitOutput(t, repo, "diff", "--cached", "--name-only"); strings.TrimSpace(got) != "" {
		t.Fatalf("cached diff should be empty after commit, got: %s", got)
	}
}

func TestStageAndCommit_PreStagedRenameClearsIndex(t *testing.T) {
	repo := initTempIndexCommitRepo(t)
	writeCommitTestFile(t, repo, "a.txt", "content\n")
	runCommitTestGit(t, repo, "add", "a.txt")
	runCommitTestGit(t, repo, "commit", "-m", "init")
	runCommitTestGit(t, repo, "mv", "a.txt", "b.txt")

	err := stageAndCommit(context.Background(), Options{
		CampaignRoot:  repo,
		PreStaged:     []string{"a.txt", "b.txt"},
		SelectiveOnly: true,
	}, "commit rename")
	if err != nil {
		t.Fatalf("stageAndCommit() error = %v", err)
	}

	if got := runCommitTestGitOutput(t, repo, "show", "HEAD:b.txt"); got != "content\n" {
		t.Fatalf("HEAD:b.txt = %q, want content", got)
	}
	if err := exec.Command("git", "-C", repo, "show", "HEAD:a.txt").Run(); err == nil {
		t.Fatal("HEAD:a.txt should not exist after rename commit")
	}
	if got := runCommitTestGitOutput(t, repo, "diff", "--cached", "--name-status"); strings.TrimSpace(got) != "" {
		t.Fatalf("cached diff should be empty after rename commit, got: %s", got)
	}
	if got := runCommitTestGitOutput(t, repo, "status", "--porcelain"); strings.TrimSpace(got) != "" {
		t.Fatalf("status should be clean after rename commit, got: %s", got)
	}
}

func TestStageAndCommit_RemovesTempIndexOnNoChanges(t *testing.T) {
	repo := initTempIndexCommitRepo(t)
	writeCommitTestFile(t, repo, "f.txt", "v1\n")
	runCommitTestGit(t, repo, "add", "f.txt")
	runCommitTestGit(t, repo, "commit", "-m", "init")

	err := stageAndCommit(context.Background(), Options{
		CampaignRoot:  repo,
		Files:         []string{"f.txt"},
		SelectiveOnly: true,
	}, "commit unchanged")
	if err == nil {
		t.Fatal("stageAndCommit() error = nil, want no changes")
	}

	matches, globErr := filepath.Glob(filepath.Join(repo, ".git", "index.tmp.*"))
	if globErr != nil {
		t.Fatalf("glob temp index files: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("temp index files were not cleaned up: %v", matches)
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
		{IntentCrawl, "Crawl"},
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

func TestNewCrawlID_UniqueAndHex(t *testing.T) {
	id1, err := NewCrawlID()
	if err != nil {
		t.Fatalf("NewCrawlID() error = %v", err)
	}
	id2, err := NewCrawlID()
	if err != nil {
		t.Fatalf("NewCrawlID() error = %v", err)
	}
	if len(id1) != crawlIDLen*2 {
		t.Errorf("crawl ID length = %d, want %d", len(id1), crawlIDLen*2)
	}
	for _, c := range id1 {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("crawl ID %q contains non-hex character %q", id1, c)
		}
	}
	if id1 == id2 {
		t.Errorf("two consecutive NewCrawlID() calls returned identical IDs: %q", id1)
	}
}

func TestCrawl_WithCrawlIDInSubject(t *testing.T) {
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

	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	crawlID, err := NewCrawlID()
	if err != nil {
		t.Fatalf("NewCrawlID() error = %v", err)
	}

	result := Crawl(context.Background(), CrawlOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		CrawlID:     crawlID,
		Description: "one item moved",
	})
	if !result.Committed {
		t.Fatalf("expected commit to succeed, got message: %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "log", "-1", "--format=%s").Output()
	if err != nil {
		t.Fatalf("failed to get git log: %v", err)
	}
	subject := strings.TrimSpace(string(out))
	if !strings.Contains(subject, "Crawl:") {
		t.Errorf("subject missing Crawl: prefix: %q", subject)
	}
	if !strings.Contains(subject, "[CW-"+crawlID+"]") {
		t.Errorf("subject missing crawl ID [CW-%s]: %q", crawlID, subject)
	}
}

func TestIntentCrawlSubject_WithAndWithoutID(t *testing.T) {
	tests := []struct {
		crawlID string
		want    string
	}{
		{"", "intent crawl completed"},
		{"abc123", "intent crawl completed [CW-abc123]"},
	}
	for _, tt := range tests {
		got := IntentCrawlSubject(tt.crawlID)
		if got != tt.want {
			t.Errorf("IntentCrawlSubject(%q) = %q, want %q", tt.crawlID, got, tt.want)
		}
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

func TestCrawl_SelectiveStaging_DirectoryTarget(t *testing.T) {
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

	initialFile := filepath.Join(tmpDir, "seed.txt")
	if err := os.WriteFile(initialFile, []byte("seed"), 0644); err != nil {
		t.Fatalf("failed to create seed file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to stage seed: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "seed").Run(); err != nil {
		t.Fatalf("failed to commit seed: %v", err)
	}

	targetDir := filepath.Join(tmpDir, "docs", "T1", "test3")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("failed to create target dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "note.md"), []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create routed file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "unrelated.txt"), []byte("unrelated"), 0644); err != nil {
		t.Fatalf("failed to create unrelated file: %v", err)
	}

	ctx := context.Background()
	result := Crawl(ctx, CrawlOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		Description: "Route directory target",
		Files:       []string{"docs/T1/test3"},
	})

	if !result.Committed {
		t.Fatalf("expected commit to succeed, got message: %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to get committed files: %v", err)
	}
	committedFiles := strings.TrimSpace(string(out))
	if !strings.Contains(committedFiles, "docs/T1/test3/note.md") {
		t.Fatalf("expected routed file in commit, got: %s", committedFiles)
	}
	if strings.Contains(committedFiles, "unrelated.txt") {
		t.Fatalf("unrelated file should not be committed: %s", committedFiles)
	}
}

func TestCrawl_SelectiveStaging_RenameScopeIncludesSourceDeletion(t *testing.T) {
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

	sourcePath := filepath.Join(tmpDir, "stale-doc.md")
	if err := os.WriteFile(sourcePath, []byte("stale"), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to stage source file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "seed").Run(); err != nil {
		t.Fatalf("failed to commit seed: %v", err)
	}

	destDir := filepath.Join(tmpDir, "dungeon", "archived")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("failed to create destination dir: %v", err)
	}
	destPath := filepath.Join(destDir, "stale-doc.md")
	if err := os.Rename(sourcePath, destPath); err != nil {
		t.Fatalf("failed to move source file: %v", err)
	}

	ctx := context.Background()
	result := Crawl(ctx, CrawlOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
		},
		Description: "Rename scope keeps source deletion",
		Files:       []string{"stale-doc.md", "dungeon/archived/stale-doc.md"},
	})

	if !result.Committed {
		t.Fatalf("expected commit to succeed, got message: %s", result.Message)
	}

	out, err := exec.Command("git", "-C", tmpDir, "diff-tree", "--no-commit-id", "--name-status", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to get committed files: %v", err)
	}
	committedFiles := string(out)
	if !strings.Contains(committedFiles, "A\tdungeon/archived/stale-doc.md") {
		t.Fatalf("expected destination addition in commit, got: %s", committedFiles)
	}
	if !strings.Contains(committedFiles, "D\tstale-doc.md") {
		t.Fatalf("expected source deletion in commit, got: %s", committedFiles)
	}

	statusOut, err := exec.Command("git", "-C", tmpDir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("failed to get git status: %v", err)
	}
	if strings.TrimSpace(string(statusOut)) != "" {
		t.Fatalf("expected clean git status after commit, got: %s", string(statusOut))
	}
}

func TestCrawl_IgnoresMissingPreStagedPathspecs(t *testing.T) {
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

	initialFile := filepath.Join(tmpDir, "seed.txt")
	if err := os.WriteFile(initialFile, []byte("seed"), 0644); err != nil {
		t.Fatalf("failed to create seed file: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "add", ".").Run(); err != nil {
		t.Fatalf("failed to stage seed: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "commit", "-m", "seed").Run(); err != nil {
		t.Fatalf("failed to commit seed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(tmpDir, "real.txt"), []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create real file: %v", err)
	}

	ctx := context.Background()
	result := Crawl(ctx, CrawlOptions{
		Options: Options{
			CampaignRoot: tmpDir,
			CampaignID:   "test1234",
			PreStaged:    []string{"missing/pathspec.txt"},
		},
		Description: "Ignore missing pathspec",
		Files:       []string{"real.txt"},
	})

	if !result.Committed {
		t.Fatalf("expected commit to succeed, got message: %s", result.Message)
	}
	if result.NoChanges {
		t.Fatal("expected a real commit, not no-changes")
	}
	if result.Err != nil {
		t.Fatalf("expected missing pre-staged path to be ignored, got err: %v", result.Err)
	}

	out, err := exec.Command("git", "-C", tmpDir, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD").Output()
	if err != nil {
		t.Fatalf("failed to get committed files: %v", err)
	}
	committedFiles := strings.TrimSpace(string(out))
	if committedFiles != "real.txt" {
		t.Fatalf("expected only real.txt in commit, got %q", committedFiles)
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

func initTempIndexCommitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runCommitTestGit(t, repo, "init")
	runCommitTestGit(t, repo, "config", "user.email", "test@test.com")
	runCommitTestGit(t, repo, "config", "user.name", "Test")
	return repo
}

func writeCommitTestFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	path := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create parent for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", rel, err)
	}
}

func readCommitTestFile(t *testing.T, repo, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repo, rel))
	if err != nil {
		t.Fatalf("failed to read %s: %v", rel, err)
	}
	return string(data)
}

func runCommitTestGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func runCommitTestGitOutput(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
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
