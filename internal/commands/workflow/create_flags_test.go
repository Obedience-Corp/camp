package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestRunCreateDefaultOutputMatchesGolden(t *testing.T) {
	goldenPath, err := filepath.Abs(filepath.Join("testdata", "create_research_human.golden"))
	if err != nil {
		t.Fatalf("resolve golden: %v", err)
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}

	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
	}); err != nil {
		t.Fatalf("runCreate: %v; stderr=%s", err, stderr.String())
	}

	if got := stdout.String(); got != string(want) {
		t.Fatalf("stdout mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRunCreateWritesCategoryMapping(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := runCreate(context.Background(), cmd, createOptions{
		Type:     "interviews",
		Shortcut: "iv",
		Title:    "Interviews",
		Category: "research",
	}); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	cfg, _, err := config.LoadCampaignConfigFromCwd(context.Background())
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if got := cfg.WorkflowCategoryForType("interviews"); got != "research" {
		t.Fatalf("category for interviews = %q, want research", got)
	}
	if got := cfg.Workflows.CategoryByType["interviews"]; got != "research" {
		t.Fatalf("category_by_type[interviews] = %q, want research", got)
	}
}

func TestRunCreateRejectsUnknownCategory(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := runCreate(context.Background(), cmd, createOptions{
		Type:     "interviews",
		Shortcut: "iv",
		Category: "bogus",
	})
	if err == nil {
		t.Fatal("expected error for unknown category, got nil")
	}
}

func TestRunCreateScaffoldsTerminalDungeonDirsWithGitkeep(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
	})
	if err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	for _, sub := range terminalDungeonDirs {
		dir := filepath.Join(root, "workflow", "research", filepath.FromSlash(sub))
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat %s: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", dir)
		}
		gitkeep := filepath.Join(dir, ".gitkeep")
		if _, err := os.Stat(gitkeep); err != nil {
			t.Fatalf("stat %s: %v", gitkeep, err)
		}
	}
	for _, sub := range []string{"inbox", "active", "ready"} {
		dir := filepath.Join(root, "workflow", "research", sub)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("live bucket %s should not be scaffolded: err=%v", sub, err)
		}
	}
}

func TestRunCreateIdempotentPrintsNoChanges(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	first := &cobra.Command{}
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	if err := runCreate(context.Background(), first, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
	}); err != nil {
		t.Fatalf("first runCreate: %v", err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	if err := runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
	}); err != nil {
		t.Fatalf("rerun runCreate: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "no changes for workflow research" {
		t.Fatalf("rerun stdout = %q, want %q", got, "no changes for workflow research")
	}
}

func TestRunCreateDryRunDoesNotWrite(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	// Touch config once so subsequent load-side default-writes are stable.
	if _, _, err := config.LoadCampaignConfigFromCwd(context.Background()); err != nil {
		t.Fatalf("preload campaign config: %v", err)
	}
	jumpsPath := filepath.Join(root, ".campaign", "settings", "jumps.yaml")
	jumpsBefore, err := os.ReadFile(jumpsPath)
	if err != nil {
		t.Fatalf("read jumps: %v", err)
	}
	workflowDir := filepath.Join(root, "workflow", "research")

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	if err := runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
		DryRun:   true,
	}); err != nil {
		t.Fatalf("dry-run runCreate: %v", err)
	}

	if !strings.Contains(stdout.String(), "plan: create workflow/research") {
		t.Fatalf("dry-run stdout missing plan header: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "dry run: nothing written") {
		t.Fatalf("dry-run stdout missing trailer: %q", stdout.String())
	}

	if _, err := os.Stat(workflowDir); !os.IsNotExist(err) {
		t.Fatalf("workflow dir was created during dry-run: err=%v", err)
	}

	jumpsAfter, err := os.ReadFile(jumpsPath)
	if err != nil {
		t.Fatalf("read jumps after dry-run: %v", err)
	}
	if !bytes.Equal(jumpsBefore, jumpsAfter) {
		t.Fatalf("dry-run modified jumps.yaml.\nbefore:\n%s\nafter:\n%s", jumpsBefore, jumpsAfter)
	}
}

func TestRunCreateJSONApplied(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	if err := runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
		JSON:     true,
	}); err != nil {
		t.Fatalf("runCreate --json: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal stdout: %v\nraw=%s", err, stdout.String())
	}
	if payload["schema_version"] != JSONSchemaVersion {
		t.Fatalf("schema_version = %v, want %s", payload["schema_version"], JSONSchemaVersion)
	}
	if payload["dry_run"] != false {
		t.Fatalf("dry_run = %v, want false", payload["dry_run"])
	}
	if payload["applied"] != true {
		t.Fatalf("applied = %v, want true", payload["applied"])
	}
	if payload["no_changes"] != false {
		t.Fatalf("no_changes = %v, want false", payload["no_changes"])
	}
	if payload["workflow_dir"] != "workflow/research/" {
		t.Fatalf("workflow_dir = %v, want workflow/research/", payload["workflow_dir"])
	}
	if payload["obey_written"] != true {
		t.Fatalf("obey_written = %v, want true", payload["obey_written"])
	}

	dirs, _ := payload["status_dirs"].([]any)
	wantDirs := statusDirsForOutput()
	if len(dirs) != len(wantDirs) {
		t.Fatalf("status_dirs length = %d, want %d (%v)", len(dirs), len(wantDirs), dirs)
	}
	for i, want := range wantDirs {
		if dirs[i] != want {
			t.Fatalf("status_dirs[%d] = %v, want %s", i, dirs[i], want)
		}
	}

	if _, err := os.Stat(filepath.Join(root, "workflow", "research", "dungeon", "completed", ".gitkeep")); err != nil {
		t.Fatalf("scaffold missing after --json apply: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "workflow", "research", "inbox")); !os.IsNotExist(err) {
		t.Fatalf("live bucket scaffolded after --json apply: err=%v", err)
	}

	jumps, err := config.LoadJumpsConfig(context.Background(), root)
	if err != nil {
		t.Fatalf("load jumps: %v", err)
	}
	if jumps.Shortcuts["re"].Path != "workflow/research/" {
		t.Fatalf("shortcut not persisted: %#v", jumps.Shortcuts)
	}
}

func TestRunCreateDryRunJSONNotApplied(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	if err := runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
		DryRun:   true,
		JSON:     true,
	}); err != nil {
		t.Fatalf("runCreate --dry-run --json: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, stdout.String())
	}
	if payload["dry_run"] != true {
		t.Fatalf("dry_run = %v, want true", payload["dry_run"])
	}
	if payload["applied"] != false {
		t.Fatalf("applied = %v, want false", payload["applied"])
	}
	if payload["obey_written"] != false {
		t.Fatalf("obey_written should be false on dry-run, got %v", payload["obey_written"])
	}

	if _, err := os.Stat(filepath.Join(root, "workflow", "research")); !os.IsNotExist(err) {
		t.Fatalf("workflow dir created during dry-run --json: err=%v", err)
	}
}

func TestRunCreateJSONIdempotent(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	first := &cobra.Command{}
	first.SetOut(&bytes.Buffer{})
	first.SetErr(&bytes.Buffer{})
	if err := runCreate(context.Background(), first, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
	}); err != nil {
		t.Fatalf("first runCreate: %v", err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	if err := runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
		JSON:     true,
	}); err != nil {
		t.Fatalf("rerun --json: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, stdout.String())
	}
	if payload["no_changes"] != true {
		t.Fatalf("no_changes = %v, want true", payload["no_changes"])
	}
	if payload["applied"] != false {
		t.Fatalf("applied = %v, want false for idempotent no-op rerun", payload["applied"])
	}
}
