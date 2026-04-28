package scaffold

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("failed to create directory %s: %v", path, err)
	}
}

func TestInit_GitInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "git-campaign")
	mustMkdirAll(t, campaignDir)

	ctx := context.Background()
	result, err := Init(ctx, campaignDir, InitOptions{Name: "git-test", NoRegister: true})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Check that git was initialized
	if !result.GitInitialized {
		t.Error("GitInitialized should be true")
	}

	// Check that .git directory exists
	gitDir := filepath.Join(campaignDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Errorf(".git directory was not created")
	}
}

func TestInit_SkipGitInit(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "no-git-campaign")
	mustMkdirAll(t, campaignDir)

	ctx := context.Background()
	result, err := Init(ctx, campaignDir, InitOptions{
		Name:        "no-git-test",
		SkipGitInit: true,
		NoRegister:  true,
	})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Check that git was not initialized
	if result.GitInitialized {
		t.Error("GitInitialized should be false when SkipGitInit is true")
	}

	// Check that .git directory does not exist
	gitDir := filepath.Join(campaignDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		t.Errorf(".git directory should not exist when SkipGitInit is true")
	}
}

func TestInit_GitAlreadyInRepo(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Create a git repo first
	gitRepoDir := filepath.Join(tmpDir, "git-repo")
	mustMkdirAll(t, gitRepoDir)

	// Initialize git in the parent directory
	cmd := exec.Command("git", "init")
	cmd.Dir = gitRepoDir
	if err := cmd.Run(); err != nil {
		t.Skipf("git not available, skipping test: %v", err)
	}

	// Create campaign dir inside the git repo
	campaignDir := filepath.Join(gitRepoDir, "campaign")
	mustMkdirAll(t, campaignDir)

	ctx := context.Background()
	result, err := Init(ctx, campaignDir, InitOptions{Name: "in-git", NoRegister: true})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Check that git was NOT initialized (already in a repo)
	if result.GitInitialized {
		t.Error("GitInitialized should be false when already inside a git repo")
	}
}

func TestInit_DryRunNoGit(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "dry-run-git")
	mustMkdirAll(t, campaignDir)

	ctx := context.Background()
	result, err := Init(ctx, campaignDir, InitOptions{
		Name:       "dry-run",
		DryRun:     true,
		NoRegister: true,
	})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Check that git was NOT initialized (dry run)
	if result.GitInitialized {
		t.Error("GitInitialized should be false in dry run mode")
	}

	// Check that .git directory was NOT created
	gitDir := filepath.Join(campaignDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		t.Errorf(".git directory should not exist in dry run mode")
	}
}

func TestInit_DescriptionAndMission(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "described-campaign")
	mustMkdirAll(t, campaignDir)

	ctx := context.Background()
	_, err := Init(ctx, campaignDir, InitOptions{
		Name:        "described",
		Description: "A well-described campaign",
		Mission:     "Build something awesome",
		NoRegister:  true,
	})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Load config and verify description and mission
	cfg, err := config.LoadCampaignConfig(ctx, campaignDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	if cfg.Description != "A well-described campaign" {
		t.Errorf("Description = %q, want %q", cfg.Description, "A well-described campaign")
	}
	if cfg.Mission != "Build something awesome" {
		t.Errorf("Mission = %q, want %q", cfg.Mission, "Build something awesome")
	}
}

func TestInit_DefaultDescription(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "default-desc")
	mustMkdirAll(t, campaignDir)

	ctx := context.Background()
	_, err := Init(ctx, campaignDir, InitOptions{
		Name:       "default-desc",
		NoRegister: true,
		// No description provided - should get default
	})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	cfg, err := config.LoadCampaignConfig(ctx, campaignDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	expectedDesc := "Campaign: default-desc"
	if cfg.Description != expectedDesc {
		t.Errorf("Description = %q, want %q (default format)", cfg.Description, expectedDesc)
	}
}

func TestInit_CreatesCanonicalIntentDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "canonical-intents")
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	ctx := context.Background()
	_, err := Init(ctx, campaignDir, InitOptions{
		Name:        "canonical-intents",
		NoRegister:  true,
		SkipGitInit: true,
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	for _, relDir := range []string{
		".campaign/intents/inbox",
		".campaign/intents/ready",
		".campaign/intents/active",
		".campaign/intents/dungeon/done",
		".campaign/intents/dungeon/killed",
		".campaign/intents/dungeon/archived",
		".campaign/intents/dungeon/someday",
	} {
		path := filepath.Join(campaignDir, filepath.FromSlash(relDir))
		if info, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected canonical intent directory %s to exist: %v", relDir, statErr)
		} else if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", relDir)
		}
	}
}
