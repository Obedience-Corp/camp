package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateObeyFiles(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create directory structure first
	for _, d := range StandardDirs {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	ctx := context.Background()
	err := CreateObeyFiles(ctx, tmpDir, false)
	if err != nil {
		t.Fatalf("CreateObeyFiles() error = %v", err)
	}

	// Check OBEY.md files exist for expected directories
	expectedDirs := []string{"projects", "worktrees", "ai_docs", "docs", "corpus", "pipelines", "code_reviews"}
	for _, d := range expectedDirs {
		obeyPath := filepath.Join(tmpDir, d, "OBEY.md")
		if _, err := os.Stat(obeyPath); os.IsNotExist(err) {
			t.Errorf("OBEY.md should exist in %s", d)
		}
	}

	// Check .campaign does NOT have an OBEY.md
	campaignObey := filepath.Join(tmpDir, ".campaign", "OBEY.md")
	if _, err := os.Stat(campaignObey); err == nil {
		t.Error(".campaign should NOT have an OBEY.md")
	}
}

func TestCreateObeyFiles_Minimal(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create minimal directory structure
	for _, d := range MinimalDirs {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	ctx := context.Background()
	err := CreateObeyFiles(ctx, tmpDir, true)
	if err != nil {
		t.Fatalf("CreateObeyFiles() error = %v", err)
	}

	// Check projects has OBEY.md
	projectsObey := filepath.Join(tmpDir, "projects", "OBEY.md")
	if _, err := os.Stat(projectsObey); os.IsNotExist(err) {
		t.Error("OBEY.md should exist in projects")
	}

	// Check worktrees does NOT exist (not in minimal)
	worktreesDir := filepath.Join(tmpDir, "worktrees")
	if _, err := os.Stat(worktreesDir); err == nil {
		t.Error("worktrees should not exist in minimal mode")
	}
}

func TestCreateObeyFiles_SkipsExisting(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create directory structure
	for _, d := range StandardDirs {
		os.MkdirAll(filepath.Join(tmpDir, d), 0755)
	}

	// Create a pre-existing OBEY.md
	existingContent := "# Existing OBEY.md\n\nDo not overwrite me!"
	existingPath := filepath.Join(tmpDir, "projects", "OBEY.md")
	os.WriteFile(existingPath, []byte(existingContent), 0644)

	ctx := context.Background()
	err := CreateObeyFiles(ctx, tmpDir, false)
	if err != nil {
		t.Fatalf("CreateObeyFiles() error = %v", err)
	}

	// Check existing OBEY.md was not overwritten
	content, err := os.ReadFile(existingPath)
	if err != nil {
		t.Fatalf("failed to read OBEY.md: %v", err)
	}
	if string(content) != existingContent {
		t.Error("existing OBEY.md was overwritten")
	}
}

func TestCreateObeyFiles_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := CreateObeyFiles(ctx, "/some/path", false)
	if err != context.Canceled {
		t.Errorf("CreateObeyFiles() error = %v, want %v", err, context.Canceled)
	}
}

func TestCreateClaudeMD(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	ctx := context.Background()
	err := CreateClaudeMD(ctx, tmpDir, "test-campaign")
	if err != nil {
		t.Fatalf("CreateClaudeMD() error = %v", err)
	}

	// Check CLAUDE.md exists
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	if _, err := os.Stat(claudePath); os.IsNotExist(err) {
		t.Error("CLAUDE.md should exist")
	}

	// Check content includes campaign name
	content, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}

	if string(content) == "" {
		t.Error("CLAUDE.md should not be empty")
	}

	// Check it contains the campaign name
	if !contains(string(content), "test-campaign") {
		t.Error("CLAUDE.md should contain the campaign name")
	}
}

func TestCreateClaudeMD_SkipsExisting(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create pre-existing CLAUDE.md
	existingContent := "# Existing CLAUDE.md\n\nDo not overwrite!"
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	os.WriteFile(claudePath, []byte(existingContent), 0644)

	ctx := context.Background()
	err := CreateClaudeMD(ctx, tmpDir, "test-campaign")
	if err != nil {
		t.Fatalf("CreateClaudeMD() error = %v", err)
	}

	// Check existing was not overwritten
	content, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}
	if string(content) != existingContent {
		t.Error("existing CLAUDE.md was overwritten")
	}
}

func TestCreateClaudeMD_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := CreateClaudeMD(ctx, "/some/path", "test")
	if err != context.Canceled {
		t.Errorf("CreateClaudeMD() error = %v, want %v", err, context.Canceled)
	}
}

func TestCreateAgentsMDSymlink(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create CLAUDE.md first (target of symlink)
	claudePath := filepath.Join(tmpDir, "CLAUDE.md")
	os.WriteFile(claudePath, []byte("# CLAUDE.md"), 0644)

	ctx := context.Background()
	err := CreateAgentsMDSymlink(ctx, tmpDir)
	if err != nil {
		t.Fatalf("CreateAgentsMDSymlink() error = %v", err)
	}

	// Check symlink exists
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	info, err := os.Lstat(agentsPath)
	if err != nil {
		t.Fatalf("failed to stat AGENTS.md: %v", err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("AGENTS.md should be a symlink")
	}

	// Check symlink target
	target, err := os.Readlink(agentsPath)
	if err != nil {
		t.Fatalf("failed to read symlink: %v", err)
	}
	if target != "CLAUDE.md" {
		t.Errorf("symlink target = %q, want %q", target, "CLAUDE.md")
	}
}

func TestCreateAgentsMDSymlink_SkipsExisting(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create pre-existing AGENTS.md (as regular file)
	agentsPath := filepath.Join(tmpDir, "AGENTS.md")
	os.WriteFile(agentsPath, []byte("# Existing"), 0644)

	ctx := context.Background()
	err := CreateAgentsMDSymlink(ctx, tmpDir)
	if err != nil {
		t.Fatalf("CreateAgentsMDSymlink() error = %v", err)
	}

	// Check it's still a regular file, not a symlink
	info, err := os.Lstat(agentsPath)
	if err != nil {
		t.Fatalf("failed to stat AGENTS.md: %v", err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("existing AGENTS.md should not be replaced with symlink")
	}
}

func TestCreateAgentsMDSymlink_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := CreateAgentsMDSymlink(ctx, "/some/path")
	if err != context.Canceled {
		t.Errorf("CreateAgentsMDSymlink() error = %v, want %v", err, context.Canceled)
	}
}

func TestCreateAgentsMDSymlink_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	err := CreateAgentsMDSymlink(ctx, "/some/path")
	if err != context.DeadlineExceeded {
		t.Errorf("CreateAgentsMDSymlink() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestObeyContentHasAllDirectories(t *testing.T) {
	// Verify ObeyContent has entries for all non-.campaign directories
	for _, d := range StandardDirs {
		if d == ".campaign" {
			continue
		}
		if _, ok := ObeyContent[d]; !ok {
			t.Errorf("ObeyContent missing entry for %s", d)
		}
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
