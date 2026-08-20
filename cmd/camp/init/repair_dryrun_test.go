package initcmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
)

// newPreFixCampaign builds a campaign whose root .gitignore predates the
// worktrees rule, which is what repair has to backfill.
func newPreFixCampaign(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	campaignDir := filepath.Join(tmpDir, "pre-fix-campaign")
	if err := os.MkdirAll(filepath.Join(campaignDir, config.CampaignDir), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.CampaignConfig{
		ID:        "test-id",
		Name:      "pre-fix-campaign",
		Type:      config.CampaignTypeProduct,
		CreatedAt: time.Now(),
	}
	if err := config.SaveCampaignConfig(context.Background(), campaignDir, cfg); err != nil {
		t.Fatalf("SaveCampaignConfig() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(campaignDir, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return campaignDir
}

// --repair --dry-run must preview the repair plan. Previewing a generic init
// instead answers a different question, and reports files as "would create"
// that repair would never touch.
func TestRunFlow_RepairDryRunPreviewsRepairPlan(t *testing.T) {
	campaignDir := newPreFixCampaign(t)

	var out bytes.Buffer
	err := RunFlow(context.Background(), Params{
		Dir:        campaignDir,
		Name:       "pre-fix-campaign",
		TypeStr:    "product",
		Repair:     true,
		DryRun:     true,
		NoRegister: true,
		NoGit:      true,
		NoSkills:   true,
	}, Writers{HumanOut: &out}, false)
	if err != nil {
		t.Fatalf("RunFlow() error = %v", err)
	}

	got := out.String()
	if !strings.Contains(got, ".gitignore") {
		t.Fatalf("repair dry run did not mention the .gitignore rule it would backfill:\n%s", got)
	}
	if strings.Contains(got, "Dry run - would create:") {
		t.Fatalf("repair dry run printed the generic init preview instead of the repair plan:\n%s", got)
	}
}

// The preview must not apply anything: dry run is the flag people reach for
// precisely because they are not ready to mutate the campaign.
func TestRunFlow_RepairDryRunWritesNothing(t *testing.T) {
	campaignDir := newPreFixCampaign(t)

	rootGitignore := filepath.Join(campaignDir, ".gitignore")
	before, err := os.ReadFile(rootGitignore)
	if err != nil {
		t.Fatal(err)
	}
	beforeEntries := dirEntryNames(t, campaignDir)

	var out bytes.Buffer
	if err := RunFlow(context.Background(), Params{
		Dir:        campaignDir,
		Name:       "pre-fix-campaign",
		TypeStr:    "product",
		Repair:     true,
		DryRun:     true,
		NoRegister: true,
		NoGit:      true,
		NoSkills:   true,
	}, Writers{HumanOut: &out}, false); err != nil {
		t.Fatalf("RunFlow() error = %v", err)
	}

	after, err := os.ReadFile(rootGitignore)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("repair dry run modified .gitignore:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if got := dirEntryNames(t, campaignDir); got != beforeEntries {
		t.Fatalf("repair dry run changed the campaign tree:\nbefore: %s\nafter:  %s", beforeEntries, got)
	}
}

func dirEntryNames(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}
