package scaffold

import (
	"context"

	"os"

	"path/filepath"

	"strings"

	"testing"

	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/quest"
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
		Name:       "test-campaign",
		Type:       config.CampaignTypeProduct,
		NoRegister: true,
	})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if result.CampaignRoot != campaignDir {
		t.Errorf("CampaignRoot = %q, want %q", result.CampaignRoot, campaignDir)
	}

	// Check key directories were created (based on templates/ structure)
	expectedDirs := []string{".campaign", "projects", "docs", "ai_docs", "dungeon", "workflow"}
	for _, dir := range expectedDirs {
		path := filepath.Join(campaignDir, dir)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("directory %s was not created", dir)
		}
	}

	// workflow/explore should be scaffolded by default.
	exploreDir := filepath.Join(campaignDir, "workflow", "explore")
	if _, err := os.Stat(exploreDir); os.IsNotExist(err) {
		t.Error("workflow/explore directory was not created")
	}

	// workflow/explore guidance should clearly differentiate from workflow/design.
	exploreObeyPath := filepath.Join(exploreDir, "OBEY.md")
	exploreObey, err := os.ReadFile(exploreObeyPath)
	if err != nil {
		t.Fatalf("failed to read workflow/explore/OBEY.md: %v", err)
	}
	if !strings.Contains(string(exploreObey), "workflow/design") {
		t.Error("workflow/explore/OBEY.md should reference workflow/design differentiation")
	}

	designObeyPath := filepath.Join(campaignDir, "workflow", "design", "OBEY.md")
	designObey, err := os.ReadFile(designObeyPath)
	if err != nil {
		t.Fatalf("failed to read workflow/design/OBEY.md: %v", err)
	}
	if !strings.Contains(string(designObey), "not a general documentation bucket") {
		t.Error("workflow/design/OBEY.md should explain that design is not a general documentation directory")
	}
	if !strings.Contains(string(designObey), "actually expect to implement") {
		t.Error("workflow/design/OBEY.md should explain that design is for implementation-bound work")
	}

	intentsObeyPath := filepath.Join(campaignDir, ".campaign", "intents", "OBEY.md")
	intentsObey, err := os.ReadFile(intentsObeyPath)
	if err != nil {
		t.Fatalf("failed to read .campaign/intents/OBEY.md: %v", err)
	}
	if !strings.Contains(string(intentsObey), ".campaign/intents/") {
		t.Error(".campaign/intents/OBEY.md should describe the canonical intent root")
	}
	if !strings.Contains(string(intentsObey), "camp intent") {
		t.Error(".campaign/intents/OBEY.md should direct operators to use camp intent")
	}
	if !strings.Contains(string(intentsObey), "workflow/explore") || !strings.Contains(string(intentsObey), "workflow/design") {
		t.Error(".campaign/intents/OBEY.md should explain its relationship to explore and design")
	}
	if strings.Contains(string(intentsObey), "workflow/intents/") {
		t.Error(".campaign/intents/OBEY.md should not describe workflow/intents as the canonical root")
	}

	workflowObeyPath := filepath.Join(campaignDir, "workflow", "OBEY.md")
	workflowObey, err := os.ReadFile(workflowObeyPath)
	if err != nil {
		t.Fatalf("failed to read workflow/OBEY.md: %v", err)
	}
	if !strings.Contains(string(workflowObey), "A pre-scaffolded directory with an `OBEY.md` file") {
		t.Error("workflow/OBEY.md should explain the required-directory meaning of OBEY.md")
	}
	if !strings.Contains(string(workflowObey), "festivals/") {
		t.Error("workflow/OBEY.md should explain how workflow planning complements festivals")
	}
	if !strings.Contains(string(workflowObey), ".campaign/intents/") {
		t.Error("workflow/OBEY.md should point intent capture at .campaign/intents/")
	}
	if strings.Contains(string(workflowObey), "workflow/intents/") {
		t.Error("workflow/OBEY.md should not present workflow/intents as a planning surface")
	}

	if _, err := os.Stat(filepath.Join(campaignDir, "workflow", "intents")); !os.IsNotExist(err) {
		t.Error("workflow/intents should not be scaffolded for new campaigns")
	}

	rootDungeonStatuses := []string{"completed", "archived", "someday"}
	for _, status := range rootDungeonStatuses {
		if _, err := os.Stat(filepath.Join(campaignDir, "dungeon", status)); os.IsNotExist(err) {
			t.Errorf("dungeon/%s directory was not created", status)
		}
	}

	standardDungeonObeys := []string{
		filepath.Join(campaignDir, "dungeon", "OBEY.md"),
		filepath.Join(campaignDir, "workflow", "code_reviews", "dungeon", "OBEY.md"),
		filepath.Join(campaignDir, "workflow", "design", "dungeon", "OBEY.md"),
		filepath.Join(campaignDir, "workflow", "explore", "dungeon", "OBEY.md"),
		filepath.Join(campaignDir, "workflow", "pipelines", "dungeon", "OBEY.md"),
	}
	for _, obeyPath := range standardDungeonObeys {
		if _, err := os.Stat(obeyPath); os.IsNotExist(err) {
			t.Errorf("standard dungeon OBEY.md was not created at %s", obeyPath)
		}
	}

	// Check key skill files were scaffolded
	expectedSkillFiles := []string{
		".campaign/skills/camp-navigation/SKILL.md",
		".campaign/skills/campaign-commit/SKILL.md",
		".campaign/skills/camp-projects/SKILL.md",
		".campaign/skills/fest-execution/SKILL.md",
	}
	for _, relPath := range expectedSkillFiles {
		path := filepath.Join(campaignDir, relPath)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("skill file %s was not created", relPath)
		}
	}

	expectedQuestPaths := []string{
		quest.RootDirName,
		filepath.Join(quest.RootDirName, quest.DefaultDirName, quest.FileName),
		filepath.Join(quest.RootDirName, "dungeon", "OBEY.md"),
		filepath.Join(quest.RootDirName, "dungeon", "completed", ".gitkeep"),
		filepath.Join(quest.RootDirName, "dungeon", "archived", ".gitkeep"),
		filepath.Join(quest.RootDirName, "dungeon", "someday", ".gitkeep"),
	}
	for _, relPath := range expectedQuestPaths {
		path := filepath.Join(campaignDir, relPath)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("quest scaffold path %s was not created", relPath)
		}
	}

	// Verify .active file is NOT created (quest context is via --quest flag or CAMP_QUEST env var)
	activePath := filepath.Join(campaignDir, quest.RootDirName, ".active")
	if _, err := os.Stat(activePath); !os.IsNotExist(err) {
		t.Errorf(".active file should not be created: %s", activePath)
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
	agentsContent, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}
	if !strings.Contains(string(agentsContent), ".campaign/intents/") {
		t.Error("AGENTS.md should point intent navigation at .campaign/intents/")
	}
	if strings.Contains(string(agentsContent), "workflow/intents/") {
		t.Error("AGENTS.md should not describe workflow/intents as the intent root")
	}

	readmePath := filepath.Join(campaignDir, "README.md")
	readmeContent, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("failed to read README.md: %v", err)
	}
	if !strings.Contains(string(readmeContent), ".campaign/          Campaign configuration and system state") {
		t.Error("README.md should describe .campaign as configuration and system state")
	}
	if !strings.Contains(string(readmeContent), "├── intents/        System-managed intent state (use camp intent)") {
		t.Error("README.md should document the canonical intent root under .campaign/")
	}
	if strings.Contains(string(readmeContent), "workflow/           Workflow management (intents, reviews, pipelines)") {
		t.Error("README.md should not describe intents as part of the workflow tree")
	}

	// Check CLAUDE.md symlink was created
	claudePath := filepath.Join(campaignDir, "CLAUDE.md")
	if _, err := os.Lstat(claudePath); os.IsNotExist(err) {
		t.Error("CLAUDE.md symlink was not created")
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
	_, err := Init(ctx, campaignDir, InitOptions{
		Name:       "dry-run",
		DryRun:     true,
		NoRegister: true,
	})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// In dry run, scaffold doesn't run so directories should NOT exist
	expectedDirs := []string{".campaign", "projects", "docs", "ai_docs", "dungeon", "workflow"}
	for _, dir := range expectedDirs {
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
	result, err := Init(ctx, campaignDir, InitOptions{NoRegister: true}) // No name specified

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
				Name:       "typed",
				Type:       campaignType,
				NoRegister: true,
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
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	// Pre-create some directories with files in them so scaffold sees them as existing
	if err := os.MkdirAll(filepath.Join(campaignDir, "projects"), 0755); err != nil {
		t.Fatalf("failed to create projects dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(campaignDir, "projects", "OBEY.md"), []byte("existing"), 0644); err != nil {
		t.Fatalf("failed to write projects/OBEY.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(campaignDir, "docs"), 0755); err != nil {
		t.Fatalf("failed to create docs dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(campaignDir, "docs", "OBEY.md"), []byte("existing"), 0644); err != nil {
		t.Fatalf("failed to write docs/OBEY.md: %v", err)
	}

	ctx := context.Background()
	result, err := Init(ctx, campaignDir, InitOptions{Name: "partial", NoRegister: true})

	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Check that skipped list contains the pre-existing files
	// Scaffold returns paths relative to dest, but init.go converts to absolute
	skippedMap := make(map[string]bool)
	for _, s := range result.Skipped {
		skippedMap[s] = true
	}

	// Check for the OBEY.md files that were pre-created
	projectsOBEY := filepath.Join(campaignDir, "projects", "OBEY.md")
	docsOBEY := filepath.Join(campaignDir, "docs", "OBEY.md")

	if !skippedMap[projectsOBEY] {
		t.Errorf("projects/OBEY.md should be in Skipped list, got: %v", result.Skipped)
	}
	if !skippedMap[docsOBEY] {
		t.Errorf("docs/OBEY.md should be in Skipped list, got: %v", result.Skipped)
	}
}
