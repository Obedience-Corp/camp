package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntentCommitAll_MissingCampaignInfo(t *testing.T) {
	ctx := context.Background()

	// Test with empty campaign root
	result := IntentCommitAll(ctx, IntentCommitOptions{
		CampaignRoot: "",
		CampaignID:   "test-id",
		Action:       IntentActionCreate,
		IntentTitle:  "Test Intent",
	})

	if result.Committed {
		t.Error("expected Committed to be false when CampaignRoot is empty")
	}
	if result.Message != "" {
		t.Errorf("expected empty message when CampaignRoot is empty, got: %s", result.Message)
	}

	// Test with empty campaign ID
	result = IntentCommitAll(ctx, IntentCommitOptions{
		CampaignRoot: "/some/path",
		CampaignID:   "",
		Action:       IntentActionCreate,
		IntentTitle:  "Test Intent",
	})

	if result.Committed {
		t.Error("expected Committed to be false when CampaignID is empty")
	}
}

func TestIntentCommitAll_TruncatesCampaignID(t *testing.T) {
	// Create a temporary git repo
	tmpDir := t.TempDir()

	// Initialize git repo
	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := context.Background()

	// Test with a long campaign ID (should be truncated to 8 chars)
	longID := "abcdefghijklmnopqrstuvwxyz123456"
	result := IntentCommitAll(ctx, IntentCommitOptions{
		CampaignRoot: tmpDir,
		CampaignID:   longID,
		Action:       IntentActionCreate,
		IntentTitle:  "Test Intent",
	})

	if !result.Committed {
		t.Errorf("expected commit to succeed, got message: %s", result.Message)
	}

	// Verify the commit message contains truncated ID
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

func TestIntentCommitAll_NoChanges(t *testing.T) {
	// Create a temporary git repo
	tmpDir := t.TempDir()

	// Initialize git repo
	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	// Create and commit an initial file so repo is not empty
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

	// Try to commit with no changes
	result := IntentCommitAll(ctx, IntentCommitOptions{
		CampaignRoot: tmpDir,
		CampaignID:   "test1234",
		Action:       IntentActionMove,
		IntentTitle:  "Test Intent",
	})

	if result.Committed {
		t.Error("expected Committed to be false when there are no changes")
	}
	if result.Message != "(no changes to commit)" {
		t.Errorf("expected 'no changes to commit' message, got: %s", result.Message)
	}
}

func TestIntentCommitAll_WithDescription(t *testing.T) {
	// Create a temporary git repo
	tmpDir := t.TempDir()

	// Initialize git repo
	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Configure git user for commits
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("failed to configure git email: %v", err)
	}
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("failed to configure git name: %v", err)
	}

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	ctx := context.Background()

	result := IntentCommitAll(ctx, IntentCommitOptions{
		CampaignRoot: tmpDir,
		CampaignID:   "test1234",
		Action:       IntentActionMove,
		IntentTitle:  "Test Intent",
		Description:  "Moved to active status",
	})

	if !result.Committed {
		t.Errorf("expected commit to succeed, got message: %s", result.Message)
	}

	// Verify the commit message contains description
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
	// Verify the action constants have expected values
	tests := []struct {
		action   IntentAction
		expected string
	}{
		{IntentActionCreate, "Create"},
		{IntentActionMove, "Move"},
		{IntentActionArchive, "Archive"},
		{IntentActionDelete, "Delete"},
		{IntentActionGather, "Gather"},
		{IntentActionPromote, "Promote"},
	}

	for _, tt := range tests {
		if string(tt.action) != tt.expected {
			t.Errorf("IntentAction %v expected %q, got %q", tt.action, tt.expected, string(tt.action))
		}
	}
}
