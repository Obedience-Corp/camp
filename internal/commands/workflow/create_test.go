package workflow

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestValidatePathSegment(t *testing.T) {
	cases := []struct {
		name  string
		value string
		ok    bool
	}{
		{name: "simple", value: "research", ok: true},
		{name: "uppercase", value: "Research", ok: true},
		{name: "dotted", value: "v1.2", ok: true},
		{name: "empty", value: "", ok: false},
		{name: "slash", value: "research/notes", ok: false},
		{name: "backslash", value: `research\notes`, ok: false},
		{name: "leading dot", value: ".research", ok: false},
		{name: "leading dash", value: "-research", ok: false},
		{name: "space", value: "research notes", ok: false},
		{name: "control", value: "research\x1fnotes", ok: false},
		{name: "too long", value: strings.Repeat("a", 81), ok: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePathSegment("type", tc.value)
			if (err == nil) != tc.ok {
				t.Fatalf("validatePathSegment(%q) error = %v, want ok %v", tc.value, err, tc.ok)
			}
		})
	}
}

func TestWriteOBEYIfMissingDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	obeyPath := filepath.Join(dir, "OBEY.md")
	const existing = "custom user workflow docs\n"
	if err := os.WriteFile(obeyPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeOBEYIfMissing(dir, "research", "Research"); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(obeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != existing {
		t.Fatalf("OBEY.md was overwritten: got %q, want %q", got, existing)
	}
}

func TestUpsertShortcutIdempotentAndNormalizesStorageKey(t *testing.T) {
	root := t.TempDir()
	cfg := &config.CampaignConfig{
		Jumps: &config.JumpsConfig{
			Shortcuts: map[string]config.ShortcutConfig{
				"RE": {Path: "workflow/research/"},
			},
		},
	}

	err := upsertShortcut(context.Background(), root, cfg, "re", "workflow/research/", "Research", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Jumps.Shortcuts["RE"]; ok {
		t.Fatal("uppercase shortcut key was not normalized away")
	}
	got, ok := cfg.Jumps.Shortcuts["re"]
	if !ok {
		t.Fatal("normalized shortcut key re was not persisted")
	}
	if got.Path != "workflow/research/" {
		t.Fatalf("shortcut path = %q, want workflow/research/", got.Path)
	}

	reloaded, err := config.LoadJumpsConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Shortcuts["RE"]; ok {
		t.Fatal("uppercase shortcut key was persisted to jumps.yaml")
	}
	if reloaded.Shortcuts["re"].Path != "workflow/research/" {
		t.Fatalf("persisted shortcut path = %q, want workflow/research/", reloaded.Shortcuts["re"].Path)
	}
}

func TestUpsertShortcutCollisionWithoutReplace(t *testing.T) {
	root := t.TempDir()
	cfg := &config.CampaignConfig{
		Jumps: &config.JumpsConfig{
			Shortcuts: map[string]config.ShortcutConfig{
				"re": {Path: "workflow/old-research/"},
			},
		},
	}

	err := upsertShortcut(context.Background(), root, cfg, "RE", "workflow/research/", "Research", false)
	if err == nil {
		t.Fatal("upsertShortcut collision returned nil, want validation error")
	}
	if !strings.Contains(err.Error(), "workflow/old-research/") {
		t.Fatalf("collision error = %q, want existing path", err)
	}
}

func TestUpsertShortcutReplaceRemovesCaseVariants(t *testing.T) {
	root := t.TempDir()
	cfg := &config.CampaignConfig{
		Jumps: &config.JumpsConfig{
			Shortcuts: map[string]config.ShortcutConfig{
				"re": {Path: "workflow/old-research/"},
				"RE": {Path: "workflow/research/"},
			},
		},
	}

	err := upsertShortcut(context.Background(), root, cfg, "RE", "workflow/new-research/", "Research", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Jumps.Shortcuts["RE"]; ok {
		t.Fatal("case-variant shortcut key was not removed")
	}
	got, ok := cfg.Jumps.Shortcuts["re"]
	if !ok {
		t.Fatal("normalized shortcut key re was not persisted")
	}
	if got.Path != "workflow/new-research/" {
		t.Fatalf("shortcut path = %q, want workflow/new-research/", got.Path)
	}

	reloaded, err := config.LoadJumpsConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Shortcuts["RE"]; ok {
		t.Fatal("case-variant shortcut key was persisted to jumps.yaml")
	}
	if reloaded.Shortcuts["re"].Path != "workflow/new-research/" {
		t.Fatalf("persisted shortcut path = %q, want workflow/new-research/", reloaded.Shortcuts["re"].Path)
	}
}

func TestUpsertConceptCollisionWithoutReplace(t *testing.T) {
	root := t.TempDir()
	cfg := &config.CampaignConfig{
		ConceptList: []config.ConceptEntry{
			{Name: "workflow", Path: "workflow/", Description: "Workflows", Children: []config.ConceptEntry{
				{Name: "Research", Path: "workflow/old-research/"},
			}},
		},
	}

	err := upsertConcept(context.Background(), root, cfg, "research", "workflow/research/", "Research", false)
	if err == nil {
		t.Fatal("upsertConcept collision returned nil, want validation error")
	}
	if !strings.Contains(err.Error(), "workflow/old-research/") {
		t.Fatalf("collision error = %q, want existing path", err)
	}
}

func TestUpsertConceptReplace(t *testing.T) {
	root := t.TempDir()
	cfg := &config.CampaignConfig{
		ConceptList: []config.ConceptEntry{
			{Name: "workflow", Path: "workflow/", Description: "Workflows", Children: []config.ConceptEntry{
				{Name: "Research", Path: "workflow/old-research/"},
			}},
		},
	}

	err := upsertConcept(context.Background(), root, cfg, "research", "workflow/research/", "Research", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ConceptList) != 1 {
		t.Fatalf("len(ConceptList) = %d, want 1 (workflow parent)", len(cfg.ConceptList))
	}
	workflow := cfg.ConceptList[0]
	if workflow.Name != "workflow" {
		t.Fatalf("top-level concept = %q, want workflow", workflow.Name)
	}
	if len(workflow.Children) != 1 || workflow.Children[0].Name != "research" {
		t.Fatalf("workflow children = %#v, want [research]", workflow.Children)
	}
	if workflow.Children[0].Path != "workflow/research/" {
		t.Fatalf("research child path = %q, want workflow/research/", workflow.Children[0].Path)
	}

	campaignYAML, err := os.ReadFile(config.CampaignConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(campaignYAML), "path: workflow/research/") {
		t.Fatalf("campaign.yaml did not persist replacement concept:\n%s", campaignYAML)
	}
}

func TestUpsertConceptFoldsLegacyFlatConcept(t *testing.T) {
	root := t.TempDir()
	cfg := &config.CampaignConfig{
		ConceptList: []config.ConceptEntry{
			{Name: "workflow", Path: "workflow/", Description: "Workflows"},
			{Name: "research", Path: "workflow/research/", Description: "Research workflow"},
		},
	}

	err := upsertConcept(context.Background(), root, cfg, "research", "workflow/research/", "Research", false)
	if err != nil {
		t.Fatalf("upsertConcept: %v", err)
	}

	if len(cfg.ConceptList) != 1 {
		t.Fatalf("len(ConceptList) = %d, want 1 (legacy flat concept folded under workflow): %#v", len(cfg.ConceptList), cfg.ConceptList)
	}
	workflow := cfg.ConceptList[0]
	if workflow.Name != "workflow" {
		t.Fatalf("top-level concept = %q, want workflow", workflow.Name)
	}
	count := 0
	for _, ch := range workflow.Children {
		if strings.EqualFold(ch.Name, "research") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("research child count = %d, want 1: %#v", count, workflow.Children)
	}
}

func TestRunCreateFoldsLegacyFlatConcept(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	cfg := &config.CampaignConfig{
		ID:   "test-campaign",
		Name: "Workflow Test",
		Type: config.CampaignTypeProduct,
		ConceptList: []config.ConceptEntry{
			{Name: "projects", Path: "projects/", Description: "Projects"},
			{Name: "research", Path: "workflow/research/", Description: "Research workflow"},
		},
	}
	if err := config.SaveCampaignConfig(context.Background(), root, cfg); err != nil {
		t.Fatal(err)
	}

	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	if err := runCreate(context.Background(), cmd, createOptions{Type: "research", Shortcut: "re", Title: "Research"}); err != nil {
		t.Fatalf("runCreate: %v", err)
	}

	reloaded, err := config.LoadCampaignConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	topLevel, child := 0, 0
	for _, c := range reloaded.ConceptList {
		if strings.EqualFold(c.Name, "research") {
			topLevel++
		}
		if strings.EqualFold(c.Name, "workflow") {
			for _, ch := range c.Children {
				if strings.EqualFold(ch.Name, "research") {
					child++
				}
			}
		}
	}
	if topLevel != 0 {
		t.Errorf("legacy flat research concept still top-level: %#v", reloaded.ConceptList)
	}
	if child != 1 {
		t.Errorf("research child count = %d, want 1: %#v", child, reloaded.ConceptList)
	}
}

func TestRunCreateRegistersExistingUserWorkflow(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	workflowDir := filepath.Join(root, "workflow", "research")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const existingOBEY = "user-authored workflow docs\n"
	if err := os.WriteFile(filepath.Join(workflowDir, "OBEY.md"), []byte(existingOBEY), 0o644); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err = runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "RE",
		Title:    "Research",
	})
	if err != nil {
		t.Fatalf("runCreate returned error: %v; stderr=%s", err, stderr.String())
	}

	if !strings.Contains(stdout.String(), "shortcut: re -> workflow/research/") {
		t.Fatalf("stdout = %q, want normalized shortcut", stdout.String())
	}
	gotOBEY, err := os.ReadFile(filepath.Join(workflowDir, "OBEY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotOBEY) != existingOBEY {
		t.Fatalf("existing OBEY.md was overwritten: got %q", gotOBEY)
	}

	jumps, err := config.LoadJumpsConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := jumps.Shortcuts["RE"]; ok {
		t.Fatal("uppercase shortcut key was persisted")
	}
	gotShortcut, ok := jumps.Shortcuts["re"]
	if !ok {
		t.Fatal("shortcut re missing from jumps.yaml")
	}
	if gotShortcut.Path != "workflow/research/" {
		t.Fatalf("shortcut path = %q, want workflow/research/", gotShortcut.Path)
	}

	cfg, err := config.LoadCampaignConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	foundConcept := false
	for _, concept := range cfg.ConceptList {
		if !strings.EqualFold(concept.Name, "workflow") {
			continue
		}
		for _, child := range concept.Children {
			if child.Name == "research" && child.Path == "workflow/research/" {
				foundConcept = true
			}
		}
	}
	if !foundConcept {
		t.Fatalf("research child concept missing from campaign config: %#v", cfg.ConceptList)
	}
}

func TestRunCreateIdempotentForSameWorkflow(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	err := runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
	})
	if err != nil {
		t.Fatalf("first runCreate returned error: %v", err)
	}

	cmd = &cobra.Command{}
	err = runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "RE",
		Title:    "Research",
	})
	if err != nil {
		t.Fatalf("second runCreate returned error: %v", err)
	}

	jumps, err := config.LoadJumpsConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := jumps.Shortcuts["RE"]; ok {
		t.Fatal("case-variant shortcut key was persisted")
	}
	if jumps.Shortcuts["re"].Path != "workflow/research/" {
		t.Fatalf("shortcut path = %q, want workflow/research/", jumps.Shortcuts["re"].Path)
	}

	cfg, err := config.LoadCampaignConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	// research nests as a child of the workflow parent, exactly once.
	count := 0
	for _, concept := range cfg.ConceptList {
		if !strings.EqualFold(concept.Name, "workflow") {
			continue
		}
		for _, child := range concept.Children {
			if strings.EqualFold(child.Name, "research") && child.Path == "workflow/research/" {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatalf("research child concept count = %d, want 1: %#v", count, cfg.ConceptList)
	}
}

func TestRunCreateReplacePersistsShortcutAndConcept(t *testing.T) {
	root := newWorkflowTestCampaign(t)
	cfg := &config.CampaignConfig{
		ID:   "test-campaign",
		Name: "Workflow Test",
		Type: config.CampaignTypeProduct,
		ConceptList: []config.ConceptEntry{
			{Name: "workflow", Path: "workflow/", Description: "Workflows", Children: []config.ConceptEntry{
				{Name: "Research", Path: "workflow/old-research/"},
			}},
		},
	}
	if err := config.SaveCampaignConfig(context.Background(), root, cfg); err != nil {
		t.Fatal(err)
	}
	jumps := &config.JumpsConfig{
		Shortcuts: map[string]config.ShortcutConfig{
			"RE": {Path: "workflow/old-research/"},
		},
	}
	if err := config.SaveJumpsConfig(context.Background(), root, jumps); err != nil {
		t.Fatal(err)
	}

	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	err := runCreate(context.Background(), cmd, createOptions{
		Type:     "research",
		Shortcut: "re",
		Title:    "Research",
		Replace:  true,
	})
	if err != nil {
		t.Fatalf("runCreate replace returned error: %v", err)
	}

	reloadedJumps, err := config.LoadJumpsConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloadedJumps.Shortcuts["RE"]; ok {
		t.Fatal("case-variant shortcut key was persisted")
	}
	if reloadedJumps.Shortcuts["re"].Path != "workflow/research/" {
		t.Fatalf("persisted shortcut path = %q, want workflow/research/", reloadedJumps.Shortcuts["re"].Path)
	}

	reloadedCfg, err := config.LoadCampaignConfig(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloadedCfg.ConceptList) != 1 {
		t.Fatalf("len(ConceptList) = %d, want 1 (workflow parent): %#v", len(reloadedCfg.ConceptList), reloadedCfg.ConceptList)
	}
	workflow := reloadedCfg.ConceptList[0]
	if workflow.Name != "workflow" {
		t.Fatalf("top-level concept = %q, want workflow", workflow.Name)
	}
	if len(workflow.Children) != 1 || workflow.Children[0].Name != "research" {
		t.Fatalf("workflow children = %#v, want [research]", workflow.Children)
	}
	if workflow.Children[0].Path != "workflow/research/" {
		t.Fatalf("research child path = %q, want workflow/research/", workflow.Children[0].Path)
	}
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}
}

func newWorkflowTestCampaign(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	campaignDir := filepath.Join(root, ".campaign")
	if err := os.MkdirAll(campaignDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const campaignYAML = `id: test-campaign
name: Workflow Test
type: product
`
	if err := os.WriteFile(filepath.Join(campaignDir, "campaign.yaml"), []byte(campaignYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
