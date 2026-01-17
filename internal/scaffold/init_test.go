package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/obediencecorp/camp/internal/config"
)

func TestInit(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Isolate registry to temp dir
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "test-campaign")
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	ctx := context.Background()
	result, err := Init(ctx, campaignDir, InitOptions{
		Name: "test-campaign",
		Type: config.CampaignTypeProduct,
	})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if result.CampaignRoot != campaignDir {
		t.Errorf("CampaignRoot = %q, want %q", result.CampaignRoot, campaignDir)
	}

	// Check directories were created
	for _, dir := range StandardDirs {
		path := filepath.Join(campaignDir, dir)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("directory %s was not created", dir)
		}
	}

	// Check campaign.yaml was created
	configPath := config.CampaignConfigPath(campaignDir)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("campaign.yaml was not created")
	}

	// Check AGENTS.md was created (source of truth)
	agentsPath := filepath.Join(campaignDir, "AGENTS.md")
	if _, err := os.Stat(agentsPath); os.IsNotExist(err) {
		t.Error("AGENTS.md was not created")
	}

	// Check CLAUDE.md symlink was created
	claudePath := filepath.Join(campaignDir, "CLAUDE.md")
	if _, err := os.Lstat(claudePath); os.IsNotExist(err) {
		t.Error("CLAUDE.md symlink was not created")
	}
}

func TestInit_Minimal(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "minimal-campaign")
	os.MkdirAll(campaignDir, 0755)

	ctx := context.Background()
	result, err := Init(ctx, campaignDir, InitOptions{
		Name:    "minimal",
		Minimal: true,
	})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Check minimal directories were created
	for _, dir := range MinimalDirs {
		path := filepath.Join(campaignDir, dir)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("directory %s was not created", dir)
		}
	}

	// Check non-minimal directories were NOT created
	nonMinimal := []string{"worktrees", "ai_docs", "docs", "corpus", "pipelines", "code_reviews"}
	for _, dir := range nonMinimal {
		path := filepath.Join(campaignDir, dir)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("non-minimal directory %s should not exist", dir)
		}
	}

	if len(result.DirsCreated) == 0 {
		t.Error("DirsCreated should not be empty")
	}
}

func TestInit_AlreadyInCampaign(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create a campaign first
	campaignDir := filepath.Join(tmpDir, "existing-campaign")
	os.MkdirAll(filepath.Join(campaignDir, config.CampaignDir), 0755)

	ctx := context.Background()

	// Try to init inside the existing campaign
	nestedDir := filepath.Join(campaignDir, "nested")
	os.MkdirAll(nestedDir, 0755)

	_, err := Init(ctx, nestedDir, InitOptions{Name: "nested"})
	if err == nil {
		t.Error("Init() should fail when already inside a campaign")
	}
}

func TestInit_CampaignExists(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignDir := filepath.Join(tmpDir, "existing")
	os.MkdirAll(filepath.Join(campaignDir, config.CampaignDir), 0755)

	ctx := context.Background()
	_, err := Init(ctx, campaignDir, InitOptions{Name: "existing"})
	if err == nil {
		t.Error("Init() should fail when .campaign already exists")
	}
}

func TestInit_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "dry-run-campaign")
	os.MkdirAll(campaignDir, 0755)

	ctx := context.Background()
	result, err := Init(ctx, campaignDir, InitOptions{
		Name:   "dry-run",
		DryRun: true,
	})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Result should have dirs listed but NOT created
	if len(result.DirsCreated) == 0 {
		t.Error("DirsCreated should list directories for dry run")
	}

	// Check directories were NOT actually created
	for _, dir := range StandardDirs {
		path := filepath.Join(campaignDir, dir)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("directory %s should not exist in dry run", dir)
		}
	}
}

func TestInit_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Init(ctx, "/some/path", InitOptions{Name: "test"})
	if err != context.Canceled {
		t.Errorf("Init() error = %v, want %v", err, context.Canceled)
	}
}

func TestInit_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := Init(ctx, "/some/path", InitOptions{Name: "test"})
	if err != context.DeadlineExceeded {
		t.Errorf("Init() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestInit_DefaultName(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "auto-named")
	os.MkdirAll(campaignDir, 0755)

	ctx := context.Background()
	result, err := Init(ctx, campaignDir, InitOptions{}) // No name specified

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Load config and check name
	cfg, err := config.LoadCampaignConfig(ctx, campaignDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	if cfg.Name != "auto-named" {
		t.Errorf("cfg.Name = %q, want %q (directory name)", cfg.Name, "auto-named")
	}

	if result.CampaignRoot != campaignDir {
		t.Errorf("CampaignRoot = %q, want %q", result.CampaignRoot, campaignDir)
	}
}

func TestInit_AllTypes(t *testing.T) {
	types := []config.CampaignType{
		config.CampaignTypeProduct,
		config.CampaignTypeResearch,
		config.CampaignTypeTools,
		config.CampaignTypePersonal,
	}

	for _, campaignType := range types {
		t.Run(string(campaignType), func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpDir, _ = filepath.EvalSymlinks(tmpDir)
			t.Setenv("XDG_CONFIG_HOME", tmpDir)

			campaignDir := filepath.Join(tmpDir, "typed-campaign")
			os.MkdirAll(campaignDir, 0755)

			ctx := context.Background()
			_, err := Init(ctx, campaignDir, InitOptions{
				Name: "typed",
				Type: campaignType,
			})

			if err != nil {
				t.Fatalf("Init() error = %v", err)
			}

			cfg, err := config.LoadCampaignConfig(ctx, campaignDir)
			if err != nil {
				t.Fatalf("LoadCampaignConfig() error = %v", err)
			}

			if cfg.Type != campaignType {
				t.Errorf("cfg.Type = %q, want %q", cfg.Type, campaignType)
			}
		})
	}
}

func TestInitOptions_Validate(t *testing.T) {
	tests := []struct {
		name    string
		opts    InitOptions
		wantErr bool
	}{
		{
			name:    "valid options",
			opts:    InitOptions{Name: "test", Type: config.CampaignTypeProduct},
			wantErr: false,
		},
		{
			name:    "empty type is valid",
			opts:    InitOptions{Name: "test"},
			wantErr: false,
		},
		{
			name:    "invalid type",
			opts:    InitOptions{Name: "test", Type: config.CampaignType("invalid")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInit_SkipsExistingDirs(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "partial-campaign")
	os.MkdirAll(campaignDir, 0755)

	// Pre-create some directories
	os.MkdirAll(filepath.Join(campaignDir, "projects"), 0755)
	os.MkdirAll(filepath.Join(campaignDir, "docs"), 0755)

	ctx := context.Background()
	result, err := Init(ctx, campaignDir, InitOptions{Name: "partial"})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// projects and docs should be in skipped
	skippedMap := make(map[string]bool)
	for _, s := range result.Skipped {
		skippedMap[s] = true
	}

	projectsPath := filepath.Join(campaignDir, "projects")
	docsPath := filepath.Join(campaignDir, "docs")

	if !skippedMap[projectsPath] {
		t.Errorf("projects should be in Skipped list")
	}
	if !skippedMap[docsPath] {
		t.Errorf("docs should be in Skipped list")
	}
}
