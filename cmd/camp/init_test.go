package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/scaffold"
)

func TestBuildRepairCommitFiles_IncludesIntentMigrations(t *testing.T) {
	result := &scaffold.InitResult{
		CampaignRoot: "/campaign",
	}
	plan := &scaffold.RepairPlan{
		IntentMigrations: []scaffold.MigrationAction{
			{
				Source: "/campaign/workflow/intents/inbox",
				Dest:   "/campaign/.campaign/intents/inbox",
				Items:  []string{"legacy.md"},
			},
		},
	}

	files := buildRepairCommitFiles(result, plan)
	got := strings.Join(files, "\n")
	if !strings.Contains(got, "workflow/intents/inbox/legacy.md") {
		t.Fatalf("commit files missing legacy source path: %v", files)
	}
	if !strings.Contains(got, ".campaign/intents/inbox/legacy.md") {
		t.Fatalf("commit files missing canonical destination path: %v", files)
	}
}

func TestBuildRepairCommitMessage_IncludesIntentMigrations(t *testing.T) {
	msg := buildRepairCommitMessage(&scaffold.InitResult{}, &scaffold.RepairPlan{
		IntentMigrations: []scaffold.MigrationAction{
			{
				Source: "/campaign/workflow/intents/inbox",
				Dest:   "/campaign/.campaign/intents/inbox",
				Items:  []string{"legacy.md"},
			},
		},
	}, 0)

	if !strings.Contains(msg, "Migrated 1 legacy intent item(s):") {
		t.Fatalf("commit message missing intent migration summary: %q", msg)
	}
	if !strings.Contains(msg, "/campaign/workflow/intents/inbox/legacy.md → /campaign/.campaign/intents/inbox") {
		t.Fatalf("commit message missing intent migration detail: %q", msg)
	}
}

// TestCampInit_NoFestOnPath verifies that camp init completes successfully
// when fest is not available, and that initializeFestivals returns the
// ErrFestNotFound sentinel so the caller can show install guidance.
//
// This test requires that fest is genuinely absent from all lookup locations.
// On a machine where fest is installed (e.g. ~/go/bin/fest), the test is
// skipped because FindFestCLI uses hardcoded fallback paths beyond PATH.
func TestCampInit_NoFestOnPath(t *testing.T) {
	// Save and clear PATH, reset the cache so FindFestCLI re-runs.
	t.Setenv("PATH", "")
	fest.ResetCache()
	defer fest.ResetCache()

	// If fest is still findable via fallback paths (e.g. ~/go/bin/fest),
	// skip rather than fail -- the behaviour under test only applies when
	// fest is completely absent.
	if fest.IsFestAvailable() {
		t.Skip("fest found at a fallback location (e.g. ~/go/bin); no-fest path cannot be exercised on this machine")
	}

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "no-path-campaign")
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	ctx := context.Background()
	opts := scaffold.InitOptions{
		Name:        "no-path-campaign",
		Type:        config.CampaignTypeProduct,
		NoRegister:  true,
		SkipGitInit: true,
	}
	if _, err := scaffold.Init(ctx, campaignDir, opts); err != nil {
		t.Fatalf("scaffold.Init() error = %v", err)
	}

	// With fest unavailable, initializeFestivals should return false + ErrFestNotFound.
	initialized, err := initializeFestivals(ctx, campaignDir, initWriters{humanOut: os.Stdout, machineOut: os.Stdout})
	if initialized {
		t.Error("initializeFestivals() should return false when fest is not available")
	}
	if err != fest.ErrFestNotFound {
		t.Errorf("initializeFestivals() error = %v, want fest.ErrFestNotFound", err)
	}
}
