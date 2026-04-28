package scaffold

import (
	"context"

	"os"

	"path/filepath"

	"testing"

	"time"

	"github.com/Obedience-Corp/camp/internal/config"

	"github.com/Obedience-Corp/camp/internal/intent"

	"github.com/Obedience-Corp/camp/internal/quest"
)

func TestInit_RepairPreservesMission(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "repair-mission")
	mustMkdirAll(t, filepath.Join(campaignDir, config.CampaignDir))

	ctx := context.Background()

	// Create initial campaign with mission
	initialCfg := &config.CampaignConfig{
		ID:          "test-id",
		Name:        "repair-mission",
		Type:        config.CampaignTypeProduct,
		Description: "Original description",
		Mission:     "Original mission",
		CreatedAt:   time.Now(),
	}
	if err := config.SaveCampaignConfig(ctx, campaignDir, initialCfg); err != nil {
		t.Fatalf("SaveCampaignConfig() error = %v", err)
	}

	// Run repair without providing new mission
	_, err := Init(ctx, campaignDir, InitOptions{
		Name:       "repair-mission",
		Repair:     true,
		NoRegister: true,
	})

	if err != nil {
		t.Fatalf("Init() with repair error = %v", err)
	}

	// Load config and verify mission was preserved
	cfg, err := config.LoadCampaignConfig(ctx, campaignDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	if cfg.Mission != "Original mission" {
		t.Errorf("Mission = %q, want %q (should preserve existing)", cfg.Mission, "Original mission")
	}
}

func TestInit_RepairMigratesLegacyIntentState(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "repair-intents")
	if err := os.MkdirAll(filepath.Join(campaignDir, config.CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	ctx := context.Background()
	initialCfg := &config.CampaignConfig{
		ID:          "repair-intents-id",
		Name:        "repair-intents",
		Type:        config.CampaignTypeProduct,
		Description: "Repair intents",
		CreatedAt:   time.Now(),
	}
	if err := config.SaveCampaignConfig(ctx, campaignDir, initialCfg); err != nil {
		t.Fatalf("SaveCampaignConfig() error = %v", err)
	}

	legacyJumps := &config.JumpsConfig{
		Paths: config.CampaignPaths{
			Workflow: "workflow/",
			Intents:  "workflow/intents/",
		},
		Shortcuts: map[string]config.ShortcutConfig{
			"i": {
				Path:        "workflow/intents/",
				Concept:     "intent",
				Description: "Legacy intents shortcut",
				Source:      config.ShortcutSourceAuto,
			},
		},
	}
	if err := config.SaveJumpsConfig(ctx, campaignDir, legacyJumps); err != nil {
		t.Fatalf("SaveJumpsConfig() error = %v", err)
	}

	legacyIntent := &intent.Intent{
		ID:        "20260316-repair-intent",
		Title:     "Repair Intent",
		Status:    intent.StatusInbox,
		Type:      intent.TypeFeature,
		CreatedAt: time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC),
		Content:   "legacy intent\n",
	}
	legacyData, err := intent.SerializeIntent(legacyIntent)
	if err != nil {
		t.Fatalf("SerializeIntent() error = %v", err)
	}

	legacyIntentPath := filepath.Join(campaignDir, "workflow", "intents", "inbox", legacyIntent.ID+".md")
	if err := os.MkdirAll(filepath.Dir(legacyIntentPath), 0755); err != nil {
		t.Fatalf("failed to create legacy intent dir: %v", err)
	}
	if err := os.WriteFile(legacyIntentPath, legacyData, 0644); err != nil {
		t.Fatalf("failed to write legacy intent: %v", err)
	}
	legacyObeyPath := filepath.Join(campaignDir, "workflow", "intents", "OBEY.md")
	legacyObey := "# legacy intent docs\n"
	if err := os.WriteFile(legacyObeyPath, []byte(legacyObey), 0644); err != nil {
		t.Fatalf("failed to write legacy intent OBEY.md: %v", err)
	}
	legacyAuditPath := filepath.Join(campaignDir, "workflow", "intents", ".intents.jsonl")
	if err := os.WriteFile(legacyAuditPath, []byte("{\"event\":\"create\"}\n"), 0644); err != nil {
		t.Fatalf("failed to write legacy audit log: %v", err)
	}

	plan, err := ComputeRepairPlan(ctx, campaignDir, InitOptions{
		Name:       "repair-intents",
		Type:       config.CampaignTypeProduct,
		Repair:     true,
		NoRegister: true,
	})
	if err != nil {
		t.Fatalf("ComputeRepairPlan() error = %v", err)
	}

	_, err = Init(ctx, campaignDir, InitOptions{
		Name:        "repair-intents",
		Type:        config.CampaignTypeProduct,
		Repair:      true,
		RepairPlan:  plan,
		NoRegister:  true,
		SkipGitInit: true,
	})
	if err != nil {
		t.Fatalf("Init() with repair error = %v", err)
	}

	canonicalIntentPath := filepath.Join(campaignDir, ".campaign", "intents", "inbox", legacyIntent.ID+".md")
	if _, err := os.Stat(canonicalIntentPath); err != nil {
		t.Fatalf("expected canonical intent at %s: %v", canonicalIntentPath, err)
	}
	if _, err := os.Stat(legacyIntentPath); !os.IsNotExist(err) {
		t.Fatalf("legacy intent should be removed after repair, err = %v", err)
	}

	canonicalAuditPath := filepath.Join(campaignDir, ".campaign", "intents", ".intents.jsonl")
	if _, err := os.Stat(canonicalAuditPath); err != nil {
		t.Fatalf("expected canonical audit log at %s: %v", canonicalAuditPath, err)
	}
	canonicalObeyPath := filepath.Join(campaignDir, ".campaign", "intents", "OBEY.md")
	canonicalObey, err := os.ReadFile(canonicalObeyPath)
	if err != nil {
		t.Fatalf("expected canonical intent marker at %s: %v", canonicalObeyPath, err)
	}
	if string(canonicalObey) != legacyObey {
		t.Fatalf("canonical intent marker = %q, want %q", string(canonicalObey), legacyObey)
	}
	if _, err := os.Stat(legacyAuditPath); !os.IsNotExist(err) {
		t.Fatalf("legacy audit log should be removed after repair, err = %v", err)
	}
	if _, err := os.Stat(legacyObeyPath); !os.IsNotExist(err) {
		t.Fatalf("legacy intent scaffold docs should be removed after repair, err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(campaignDir, "workflow", "intents")); !os.IsNotExist(err) {
		t.Fatalf("legacy workflow/intents tree should be removed after repair, err = %v", err)
	}
}

func TestInit_RepairUpdatesMission(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "repair-update")
	mustMkdirAll(t, filepath.Join(campaignDir, config.CampaignDir))

	ctx := context.Background()

	// Create initial campaign with mission
	initialCfg := &config.CampaignConfig{
		ID:          "test-id",
		Name:        "repair-update",
		Type:        config.CampaignTypeProduct,
		Description: "Original description",
		Mission:     "Original mission",
		CreatedAt:   time.Now(),
	}
	if err := config.SaveCampaignConfig(ctx, campaignDir, initialCfg); err != nil {
		t.Fatalf("SaveCampaignConfig() error = %v", err)
	}

	// Run repair with new mission
	_, err := Init(ctx, campaignDir, InitOptions{
		Name:       "repair-update",
		Mission:    "Updated mission via repair",
		Repair:     true,
		NoRegister: true,
	})

	if err != nil {
		t.Fatalf("Init() with repair error = %v", err)
	}

	// Load config and verify mission was updated
	cfg, err := config.LoadCampaignConfig(ctx, campaignDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	if cfg.Mission != "Updated mission via repair" {
		t.Errorf("Mission = %q, want %q (should update)", cfg.Mission, "Updated mission via repair")
	}
}

func TestInit_RepairPreservesLegacyConceptList(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "repair-legacy-concepts")
	if err := os.MkdirAll(filepath.Join(campaignDir, config.CampaignDir), 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	ctx := context.Background()

	initialCfg := &config.CampaignConfig{
		ID:          "legacy-id",
		Name:        "repair-legacy-concepts",
		Type:        config.CampaignTypeProduct,
		Description: "Legacy concept config",
		Mission:     "Keep legacy concepts",
		CreatedAt:   time.Now(),
		ConceptList: []config.ConceptEntry{
			{Name: "design", Path: "workflow/design/", Description: "Design"},
			{Name: "dungeon", Path: "dungeon/", Description: "Legacy dungeon concept"},
		},
	}
	if err := config.SaveCampaignConfig(ctx, campaignDir, initialCfg); err != nil {
		t.Fatalf("SaveCampaignConfig() error = %v", err)
	}

	_, err := Init(ctx, campaignDir, InitOptions{
		Name:       "repair-legacy-concepts",
		Repair:     true,
		NoRegister: true,
	})
	if err != nil {
		t.Fatalf("Init() with repair error = %v", err)
	}

	cfg, err := config.LoadCampaignConfig(ctx, campaignDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	if len(cfg.ConceptList) != 2 {
		t.Fatalf("len(ConceptList) = %d, want 2", len(cfg.ConceptList))
	}

	var hasDungeon bool
	for _, c := range cfg.ConceptList {
		if c.Name == "dungeon" && c.Path == "dungeon/" {
			hasDungeon = true
			break
		}
	}
	if !hasDungeon {
		t.Fatal("repair should preserve explicit legacy dungeon concept entry")
	}
}

func TestInit_RepairRestoresQuestScaffold(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "repair-quest")
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	ctx := context.Background()
	if _, err := Init(ctx, campaignDir, InitOptions{
		Name:       "repair-quest",
		NoRegister: true,
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	removed := []string{
		filepath.Join(campaignDir, quest.RootDirName, quest.DefaultDirName, quest.FileName),
		filepath.Join(campaignDir, quest.RootDirName, "dungeon", "OBEY.md"),
	}
	for _, path := range removed {
		if err := os.Remove(path); err != nil {
			t.Fatalf("os.Remove(%q) error = %v", path, err)
		}
	}

	if _, err := Init(ctx, campaignDir, InitOptions{
		Name:       "repair-quest",
		Repair:     true,
		NoRegister: true,
	}); err != nil {
		t.Fatalf("Init() with repair error = %v", err)
	}

	for _, path := range removed {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected repaired quest scaffold path to exist: %s", path)
		}
	}

	// Verify .active file is NOT recreated on repair
	activePath := filepath.Join(campaignDir, quest.RootDirName, ".active")
	if _, err := os.Stat(activePath); !os.IsNotExist(err) {
		t.Errorf(".active file should not be created on repair: %s", activePath)
	}
}
