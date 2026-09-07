package fresh

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func setRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, config.CampaignDir, config.SettingsDir), 0o755); err != nil {
		t.Fatalf("create settings dir: %v", err)
	}
	return root
}

func TestApplyFreshSettingWritesGlobalPrune(t *testing.T) {
	ctx := context.Background()
	root := setRoot(t)

	result, err := applyFreshSetting(ctx, root, freshSetInput{Key: "prune", Action: "off"})
	if err != nil {
		t.Fatalf("set prune off: %v", err)
	}
	if !result.Changed {
		t.Fatal("first write should change the file")
	}
	cfg, err := config.LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Prune == nil || *cfg.Prune {
		t.Fatalf("prune = %v, want false", cfg.Prune)
	}

	again, err := applyFreshSetting(ctx, root, freshSetInput{Key: "prune", Action: "off"})
	if err != nil {
		t.Fatalf("set prune off again: %v", err)
	}
	if again.Changed {
		t.Fatalf("unchanged write reported changed: %+v", again)
	}
}

func TestApplyFreshSettingRefusesProjectPrune(t *testing.T) {
	_, err := applyFreshSetting(context.Background(), setRoot(t), freshSetInput{
		Key:     "prune",
		Action:  "off",
		Project: "api",
	})
	if err == nil {
		t.Fatal("project prune write should fail")
	}
}

func TestApplyFreshSettingProjectBranch(t *testing.T) {
	ctx := context.Background()
	root := setRoot(t)

	_, err := applyFreshSetting(ctx, root, freshSetInput{
		Key:     "branch",
		Action:  "branch",
		Value:   "feat/api",
		Project: "api",
	})
	if err != nil {
		t.Fatalf("set project branch: %v", err)
	}
	cfg, err := config.LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	pc := cfg.Projects["api"]
	if pc.Branch == nil || *pc.Branch != "feat/api" {
		t.Fatalf("project branch = %v, want feat/api", pc.Branch)
	}

	_, err = applyFreshSetting(ctx, root, freshSetInput{
		Key:     "branch",
		Action:  "inherit",
		Project: "api",
	})
	if err != nil {
		t.Fatalf("clear project branch: %v", err)
	}
	cfg, err = config.LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("reload after inherit: %v", err)
	}
	if _, ok := cfg.Projects["api"]; ok {
		t.Fatalf("cleared project should drop empty scope, got %+v", cfg.Projects["api"])
	}
}

func TestApplyFreshSettingRejectsEmptyBranch(t *testing.T) {
	_, err := applyFreshSetting(context.Background(), setRoot(t), freshSetInput{
		Key:    "branch",
		Action: "branch",
	})
	if err == nil {
		t.Fatal("empty --value should fail")
	}
}

func TestReplaceFreshFollowUpUpdatesInPlace(t *testing.T) {
	ctx := context.Background()
	root := setRoot(t)
	if err := config.AddFreshFollowUp(ctx, root, "", config.FollowUpConfig{Name: "install", Run: "npm install"}); err != nil {
		t.Fatalf("seed follow-up: %v", err)
	}
	if err := config.AddFreshFollowUp(ctx, root, "", config.FollowUpConfig{Name: "build", Run: "npm run build"}); err != nil {
		t.Fatalf("seed second follow-up: %v", err)
	}
	cfg, err := config.LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := replaceFreshFollowUp(ctx, cfg, root, "", "install", config.FollowUpConfig{
		Name:            "bootstrap",
		Run:             "npm ci",
		ContinueOnError: true,
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	cfg, err = config.LoadFreshConfig(ctx, root)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(cfg.FollowUp) != 2 {
		t.Fatalf("follow-ups = %d, want 2", len(cfg.FollowUp))
	}
	if cfg.FollowUp[0].Name != "bootstrap" || cfg.FollowUp[0].Run != "npm ci" || !cfg.FollowUp[0].ContinueOnError {
		t.Fatalf("first follow-up = %+v", cfg.FollowUp[0])
	}
	if cfg.FollowUp[1].Name != "build" {
		t.Fatalf("order lost: %+v", cfg.FollowUp)
	}
}

func TestApplyFreshSettingRejectsUnknownKey(t *testing.T) {
	_, err := applyFreshSetting(context.Background(), setRoot(t), freshSetInput{
		Key:    "merged_workitems",
		Action: "on",
	})
	if err == nil {
		t.Fatal("unknown key should fail")
	}
}
