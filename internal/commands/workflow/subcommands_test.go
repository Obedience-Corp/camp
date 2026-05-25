package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
)

func createResearch(t *testing.T, _ string) {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
	}); err != nil {
		t.Fatalf("seed runCreate: %v", err)
	}
}

func runJSON(t *testing.T, args []string, fn func(*cobra.Command) error) map[string]any {
	t.Helper()
	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := fn(cmd); err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal %v: %v\nraw=%s", args, err, stdout.String())
	}
	return payload
}

func TestList_EmptyAndPopulated(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runList(context.Background(), cmd, false); err != nil {
		t.Fatalf("runList empty: %v", err)
	}
	if !strings.Contains(stdout.String(), "no user-created workflows") {
		t.Fatalf("empty list stdout = %q", stdout.String())
	}

	createResearch(t, root)

	stdout.Reset()
	if err := runList(context.Background(), cmd, false); err != nil {
		t.Fatalf("runList populated: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"TYPE", "SHORTCUT", "research", "re"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list table missing %q in:\n%s", want, out)
		}
	}

	payload := runJSON(t, []string{"list", "--json"}, func(c *cobra.Command) error {
		return runList(context.Background(), c, true)
	})
	if payload["schema_version"] != JSONSchemaVersion {
		t.Fatalf("schema_version = %v", payload["schema_version"])
	}
	wf, ok := payload["workflows"].([]any)
	if !ok || len(wf) != 1 {
		t.Fatalf("workflows = %v", payload["workflows"])
	}
	first := wf[0].(map[string]any)
	if first["type"] != "research" || first["shortcut"] != "re" {
		t.Fatalf("first workflow = %v", first)
	}
}

func TestShow_NotFoundReturnsError(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runShow(context.Background(), cmd, "nope", false)
	if err == nil {
		t.Fatalf("runShow missing type should error")
	}
	if !errors.Is(err, errWorkflowNotFound) {
		t.Fatalf("err = %v, want errWorkflowNotFound", err)
	}
}

func TestShow_FoundIncludesScaffoldInfo(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	createResearch(t, root)

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runShow(context.Background(), cmd, "research", false); err != nil {
		t.Fatalf("runShow: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{"workflow: research", "path: workflow/research/", "shortcut: re -> workflow/research/"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show stdout missing %q in:\n%s", want, out)
		}
	}

	payload := runJSON(t, []string{"show", "research", "--json"}, func(c *cobra.Command) error {
		return runShow(context.Background(), c, "research", true)
	})
	if payload["type"] != "research" || payload["shortcut"] != "re" {
		t.Fatalf("show json = %v", payload)
	}
}

func TestShortcutAdd_NoChangeOnExistingMatch(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	createResearch(t, root)

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runShortcutAdd(context.Background(), cmd, "research", "re", false, false); err != nil {
		t.Fatalf("runShortcutAdd: %v", err)
	}
	if !strings.Contains(stdout.String(), "no changes for shortcut re") {
		t.Fatalf("expected no-change message, got %q", stdout.String())
	}
}

func TestShortcutAdd_CollisionRequiresReplace(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	createResearch(t, root)

	// Manually create a second workflow dir without a shortcut.
	otherDir := filepath.Join(root, "workflow", "feature")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.LoadCampaignConfigFromCwd(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConceptList = append(cfg.ConceptList, config.ConceptEntry{
		Name: "feature", Path: "workflow/feature/", Description: "feature workflow",
	})
	if err := config.SaveCampaignConfig(context.Background(), root, cfg); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err = runShortcutAdd(context.Background(), cmd, "feature", "re", false, false)
	if err == nil {
		t.Fatalf("expected collision error")
	}
	if !strings.Contains(err.Error(), "already points to") {
		t.Fatalf("err = %v, want collision message", err)
	}

	// --replace should succeed.
	if err := runShortcutAdd(context.Background(), cmd, "feature", "re", true, false); err != nil {
		t.Fatalf("replace runShortcutAdd: %v", err)
	}
	jumps, err := config.LoadJumpsConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if jumps.Shortcuts["re"].Path != "workflow/feature/" {
		t.Fatalf("shortcut not replaced: %#v", jumps.Shortcuts)
	}
}

func TestShortcutAdd_UnknownTypeReturnsError(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runShortcutAdd(context.Background(), cmd, "nope", "no", false, false)
	if !errors.Is(err, errWorkflowNotFound) {
		t.Fatalf("err = %v, want errWorkflowNotFound", err)
	}
}

func TestDoctor_CleanRepoNoFindings(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	createResearch(t, root)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := runDoctor(context.Background(), cmd, false); err != nil {
		t.Fatalf("doctor clean: %v", err)
	}
}

func TestDoctor_ShortcutMissingTargetIsError(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	createResearch(t, root)
	if err := os.RemoveAll(filepath.Join(root, "workflow", "research")); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runDoctor(context.Background(), cmd, false)
	if !errors.Is(err, errDoctorIssuesFound) {
		t.Fatalf("doctor err = %v, want errDoctorIssuesFound", err)
	}
}

func TestDoctor_DirMissingShortcutIsInfoOnly(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	// Workflow dir + concept but no shortcut.
	dir := filepath.Join(root, "workflow", "feature")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.LoadCampaignConfigFromCwd(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConceptList = append(cfg.ConceptList, config.ConceptEntry{
		Name: "feature", Path: "workflow/feature/",
	})
	if err := config.SaveCampaignConfig(context.Background(), root, cfg); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runDoctor(context.Background(), cmd, false); err != nil {
		t.Fatalf("doctor returned error for info-only findings: %v", err)
	}
	if !strings.Contains(stdout.String(), codeDirMissingShortcut) {
		t.Fatalf("expected %s finding, got:\n%s", codeDirMissingShortcut, stdout.String())
	}
}

func TestDoctor_JSONShape(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	createResearch(t, root)
	if err := os.RemoveAll(filepath.Join(root, "workflow", "research")); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	_ = runDoctor(context.Background(), cmd, true) // ignore err: we just want to inspect output

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, stdout.String())
	}
	if int(payload["error_count"].(float64)) < 1 {
		t.Fatalf("expected error_count >= 1, got %v", payload["error_count"])
	}
}

func TestSync_DryRunReportsPlans(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	createResearch(t, root)
	if err := os.RemoveAll(filepath.Join(root, "workflow", "research")); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runSync(context.Background(), cmd, false, false); err != nil {
		t.Fatalf("sync dry-run: %v", err)
	}
	if !strings.Contains(stdout.String(), "would fix") {
		t.Fatalf("dry-run stdout missing plan message: %q", stdout.String())
	}

	jumps, err := config.LoadJumpsConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := jumps.Shortcuts["re"]; !ok {
		t.Fatalf("dry-run removed shortcut: %#v", jumps.Shortcuts)
	}
}

func TestSync_ApplyRepairsShortcutAndConcept(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	createResearch(t, root)
	if err := os.RemoveAll(filepath.Join(root, "workflow", "research")); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := runSync(context.Background(), cmd, true, false); err != nil {
		t.Fatalf("sync apply: %v", err)
	}

	jumps, err := config.LoadJumpsConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := jumps.Shortcuts["re"]; ok {
		t.Fatalf("apply did not remove orphan shortcut: %#v", jumps.Shortcuts)
	}

	cfg, err := config.LoadCampaignConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cfg.ConceptList {
		if strings.EqualFold(c.Name, "research") {
			t.Fatalf("apply did not remove orphan concept: %#v", c)
		}
	}

	if err := runDoctor(context.Background(), cmd, false); err != nil {
		t.Fatalf("doctor still reports errors after sync --apply: %v", err)
	}
}

func TestSync_NoChangesOnCleanRepo(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	createResearch(t, root)

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runSync(context.Background(), cmd, false, false); err != nil {
		t.Fatalf("sync clean: %v", err)
	}
	if !strings.Contains(stdout.String(), "nothing to fix") {
		t.Fatalf("clean stdout = %q", stdout.String())
	}
}
