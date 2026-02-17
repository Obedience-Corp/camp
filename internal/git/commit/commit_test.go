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
