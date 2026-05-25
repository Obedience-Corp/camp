package workitem

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
)

// stagingTestCampaign provides a git-initialized campaign with a single
// design workitem under workflow/design/example/. Mirrors the linkTestCampaign
// scaffold but additionally runs `git init` so the planner can call status.
func stagingTestCampaign(t *testing.T) string {
	t.Helper()
	rawRoot := t.TempDir()
	root, err := filepath.EvalSymlinks(rawRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"),
		[]byte("id: test-campaign\nname: Test\ntype: product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wiDir := filepath.Join(root, "workflow", "design", "example")
	if err := os.MkdirAll(wiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const meta = `version: v1alpha6
kind: workitem
id: design-example-2026-05-24
type: design
title: Example
ref: WI-abc123
`
	if err := os.WriteFile(filepath.Join(wiDir, ".workitem"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "projects", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@example.com")
	runGit(t, root, "config", "user.name", "Test")
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-q", "-m", "initial")
	return root
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestComputePlan_WorkitemDir_Ancestor(t *testing.T) {
	root := stagingTestCampaign(t)
	defer chdir(t, filepath.Join(root, "workflow", "design", "example"))()

	if err := os.WriteFile(filepath.Join(root, "workflow", "design", "example", "notes.md"),
		[]byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := ComputePlan(context.Background(), root, PlanOptions{CampaignID: "test-campaign"})
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if plan.Context != PlanContextWorkitemDir {
		t.Fatalf("context = %q, want %q", plan.Context, PlanContextWorkitemDir)
	}
	if !contains(plan.Stage, "workflow/design/example/notes.md") {
		t.Fatalf("stage missing notes.md: %v", plan.Stage)
	}
	if !strings.Contains(plan.Tag, "WI-WI-abc123") {
		t.Fatalf("tag missing workitem ref: %s", plan.Tag)
	}
}

func TestComputePlan_NoContext_Errors(t *testing.T) {
	root := stagingTestCampaign(t)
	defer chdir(t, root)()

	_, err := ComputePlan(context.Background(), root, PlanOptions{CampaignID: "test-campaign"})
	if !errors.Is(err, ErrNoWorkitemContext) {
		t.Fatalf("err = %v, want ErrNoWorkitemContext", err)
	}
}

func TestComputePlan_ExplicitWorkitem(t *testing.T) {
	root := stagingTestCampaign(t)
	defer chdir(t, root)()

	if err := os.WriteFile(filepath.Join(root, "workflow", "design", "example", "spec.md"),
		[]byte("spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := ComputePlan(context.Background(), root, PlanOptions{
		Explicit:   "design-example-2026-05-24",
		CampaignID: "test-campaign",
	})
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if plan.Context != PlanContextCampaignRoot {
		t.Fatalf("context = %q, want %q", plan.Context, PlanContextCampaignRoot)
	}
	if !contains(plan.Stage, "workflow/design/example/spec.md") {
		t.Fatalf("stage missing spec.md: %v", plan.Stage)
	}
}

func TestComputePlan_ExcludeRemovesPath(t *testing.T) {
	root := stagingTestCampaign(t)
	defer chdir(t, filepath.Join(root, "workflow", "design", "example"))()

	if err := os.WriteFile(filepath.Join(root, "workflow", "design", "example", "a.md"),
		[]byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workflow", "design", "example", "b.md"),
		[]byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := ComputePlan(context.Background(), root, PlanOptions{
		Excludes:   []string{"workflow/design/example/b.md"},
		CampaignID: "test-campaign",
	})
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if contains(plan.Stage, "workflow/design/example/b.md") {
		t.Fatalf("exclude failed; stage = %v", plan.Stage)
	}
	if !contains(plan.Stage, "workflow/design/example/a.md") {
		t.Fatalf("a.md missing from stage = %v", plan.Stage)
	}
	if !skipContains(plan.Skip, "workflow/design/example/b.md", skipReasonExcludeFlag) {
		t.Fatalf("excluded path not in skip list: %#v", plan.Skip)
	}
}

func TestComputePlan_IncludeAddsExplicitPath(t *testing.T) {
	root := stagingTestCampaign(t)
	defer chdir(t, filepath.Join(root, "workflow", "design", "example"))()

	if err := os.WriteFile(filepath.Join(root, "workflow", "design", "example", "main.md"),
		[]byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs.md"), []byte("docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan, err := ComputePlan(context.Background(), root, PlanOptions{
		Includes:   []string{"docs.md"},
		CampaignID: "test-campaign",
	})
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if !contains(plan.Stage, "docs.md") {
		t.Fatalf("include path missing: %v", plan.Stage)
	}
}

func TestComputePlan_StagedShortCircuits(t *testing.T) {
	root := stagingTestCampaign(t)
	defer chdir(t, filepath.Join(root, "workflow", "design", "example"))()

	if err := os.WriteFile(filepath.Join(root, "x.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "x.md")

	plan, err := ComputePlan(context.Background(), root, PlanOptions{
		StagedOnly: true,
		CampaignID: "test-campaign",
	})
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if plan.Context != PlanContextStagedOnly {
		t.Fatalf("context = %q", plan.Context)
	}
	if !contains(plan.PreStaged, "x.md") {
		t.Fatalf("staged file missing from PreStaged: %v", plan.PreStaged)
	}
	if len(plan.Stage) != 0 {
		t.Fatalf("Stage should be empty under --staged, got %v", plan.Stage)
	}
}

func TestComputePlan_LinkedProject(t *testing.T) {
	root := stagingTestCampaign(t)
	demoDir := filepath.Join(root, "projects", "demo")
	runGit(t, demoDir, "init", "-q")
	runGit(t, demoDir, "config", "user.email", "test@example.com")
	runGit(t, demoDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(demoDir, "seed.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, demoDir, "add", "-A")
	runGit(t, demoDir, "commit", "-q", "-m", "seed")

	registry := links.Links{
		Version: links.LinksSchemaVersion,
		Links: []links.Link{{
			ID:         "lnk_20260524_aaaaaa",
			WorkitemID: "design-example-2026-05-24",
			Scope:      links.LinkScope{Kind: links.ScopeProject, Path: "projects/demo"},
			Role:       links.RolePrimary,
		}},
	}
	if err := links.Save(context.Background(), root, &registry); err != nil {
		t.Fatalf("save links: %v", err)
	}

	if err := os.WriteFile(filepath.Join(demoDir, "feature.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer chdir(t, demoDir)()

	plan, err := ComputePlan(context.Background(), root, PlanOptions{CampaignID: "test-campaign"})
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if plan.Context != PlanContextLinkedProject {
		t.Fatalf("context = %q, want %q", plan.Context, PlanContextLinkedProject)
	}
	if plan.RepoRoot != demoDir {
		t.Fatalf("repo root = %q, want %q", plan.RepoRoot, demoDir)
	}
	if !contains(plan.Stage, "feature.go") {
		t.Fatalf("stage missing feature.go: %v", plan.Stage)
	}
}

func TestComputePlan_ProjectFlagOverridesResolver(t *testing.T) {
	root := stagingTestCampaign(t)
	demoDir := filepath.Join(root, "projects", "demo")
	runGit(t, demoDir, "init", "-q")
	runGit(t, demoDir, "config", "user.email", "test@example.com")
	runGit(t, demoDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(demoDir, "z.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	defer chdir(t, filepath.Join(root, "workflow", "design", "example"))()

	plan, err := ComputePlan(context.Background(), root, PlanOptions{
		Explicit:   "design-example-2026-05-24",
		Project:    "demo",
		CampaignID: "test-campaign",
	})
	if err != nil {
		t.Fatalf("ComputePlan: %v", err)
	}
	if plan.Context != PlanContextLinkedProject {
		t.Fatalf("context = %q", plan.Context)
	}
	if plan.RepoRoot != demoDir {
		t.Fatalf("repo root = %q, want %q", plan.RepoRoot, demoDir)
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func skipContains(haystack []SkippedEntry, path, reason string) bool {
	for _, e := range haystack {
		if e.Path == path && e.Reason == reason {
			return true
		}
	}
	return false
}
