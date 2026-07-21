package workitem

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
)

func TestResolve_NoneWhenEmpty(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := newCmd()
	if err := runResolve(context.Background(), cmd, resolveOptions{}); err != nil {
		t.Fatalf("runResolve: %v", err)
	}
}

func TestResolve_ExplicitTierWins(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	res, err := resolver.Resolve(context.Background(), root, resolver.Options{
		Explicit: "design-example-2026-05-24",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Source != resolver.SourceExplicit {
		t.Fatalf("source = %s, want explicit", res.Source)
	}
}

func TestResolve_AncestorTier(t *testing.T) {
	root := linkTestCampaign(t)
	cwd := filepath.Join(root, "workflow", "design", "example")
	restore := chdir(t, cwd)
	defer restore()

	res, err := resolver.Resolve(context.Background(), root, resolver.Options{Cwd: cwd})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Source != resolver.SourceAncestor {
		t.Fatalf("source = %s, want ancestor; trace=%v", res.Source, res.Trace)
	}
}

func TestResolve_AncestorTierWithSymlinkedCwd(t *testing.T) {
	root := linkTestCampaign(t)
	linkRoot := filepath.Join(t.TempDir(), "campaign-link")
	if err := os.Symlink(root, linkRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	cwd := filepath.Join(linkRoot, "workflow", "design", "example")

	res, err := resolver.Resolve(context.Background(), root, resolver.Options{Cwd: cwd})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Source != resolver.SourceAncestor {
		t.Fatalf("source = %s, want ancestor; trace=%v", res.Source, res.Trace)
	}
}

func TestResolve_LinkTier(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	// Create a primary link on projects/demo, then resolve from inside it.
	cmd := newCmd()
	if err := runLink(context.Background(), cmd, linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	cwd := filepath.Join(root, "projects", "demo")
	res, err := resolver.Resolve(context.Background(), root, resolver.Options{Cwd: cwd})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Source != resolver.SourceLink {
		t.Fatalf("source = %s, want link; trace=%v", res.Source, res.Trace)
	}
}

func TestResolve_BrokenPrimaryLinkFallsThroughToLowerTier(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	if err := runLink(context.Background(), newCmd(), linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	if err := links.SaveCurrent(context.Background(), root, &links.Current{
		Version:    links.CurrentSchemaVersion,
		WorkitemID: "design-example-2026-05-24",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "workflow", "design", "example")); err != nil {
		t.Fatal(err)
	}

	cwd := filepath.Join(root, "projects", "demo")
	res, err := resolver.Resolve(context.Background(), root, resolver.Options{Cwd: cwd})
	if err != nil {
		t.Fatalf("resolver should not error on broken primary link, got %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	sawError := false
	for _, step := range res.Trace {
		if step.Result == "error" {
			sawError = true
			break
		}
	}
	if !sawError {
		t.Fatalf("expected an error trace step from the broken link tier, got %#v", res.Trace)
	}
}

func TestResolve_AncestorBeatsLink(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	// Create link, then cd into the workitem dir (ancestor tier should fire).
	if err := runLink(context.Background(), newCmd(), linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cwd := filepath.Join(root, "workflow", "design", "example")
	res, err := resolver.Resolve(context.Background(), root, resolver.Options{Cwd: cwd})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Source != resolver.SourceAncestor {
		t.Fatalf("ancestor must beat link; got source=%s trace=%v", res.Source, res.Trace)
	}
}

func TestResolve_CurrentTierFallback(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cur := &links.Current{
		Version:    links.CurrentSchemaVersion,
		WorkitemID: "design-example-2026-05-24",
	}
	if err := links.SaveCurrent(context.Background(), root, cur); err != nil {
		t.Fatal(err)
	}
	// cd to a path with no .workitem ancestor and no link match.
	otherDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := resolver.Resolve(context.Background(), root, resolver.Options{Cwd: otherDir})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Source != resolver.SourceCurrent {
		t.Fatalf("source = %s, want current; trace=%v", res.Source, res.Trace)
	}
}

func TestResolve_DisableCurrentSkipsCurrentTier(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cur := &links.Current{
		Version:    links.CurrentSchemaVersion,
		WorkitemID: "design-example-2026-05-24",
	}
	if err := links.SaveCurrent(context.Background(), root, cur); err != nil {
		t.Fatal(err)
	}
	otherDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := resolver.Resolve(context.Background(), root, resolver.Options{
		Cwd:            otherDir,
		DisableCurrent: true,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Source != resolver.SourceNone {
		t.Fatalf("source = %s, want none; trace=%v", res.Source, res.Trace)
	}
	last := res.Trace[len(res.Trace)-2]
	if last.Tier != resolver.SourceCurrent || last.Result != "skip" {
		t.Fatalf("current trace = %+v, want a skipped current tier", last)
	}
}

func TestResolve_FestivalTier(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	festDir := filepath.Join(root, "festivals", "active", "CT0001")
	if err := os.MkdirAll(festDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runLink(context.Background(), newCmd(), linkOptions{
		Selector: "design-example-2026-05-24",
		Festival: "CT0001",
	}); err != nil {
		t.Fatalf("seed festival link: %v", err)
	}

	// cwd is the campaign root, no ancestor, no path-link match.
	res, err := resolver.Resolve(context.Background(), root, resolver.Options{
		Cwd:        root,
		FestivalID: "CT0001",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Source != resolver.SourceFestival {
		t.Fatalf("source = %s, want festival; trace=%v", res.Source, res.Trace)
	}
}

func TestResolve_NoneSource(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	otherDir := filepath.Join(root, "docs")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := resolver.Resolve(context.Background(), root, resolver.Options{Cwd: otherDir})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Source != resolver.SourceNone {
		t.Fatalf("source = %s, want none; trace=%v", res.Source, res.Trace)
	}
}

func TestResolve_JSONShape(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runResolve(context.Background(), cmd, resolveOptions{
		Explicit: "design-example-2026-05-24",
		JSON:     true,
	}); err != nil {
		t.Fatalf("runResolve --json: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, stdout.String())
	}
	if payload["schema_version"] != "workitem-resolve/v1alpha1" {
		t.Fatalf("schema_version = %v", payload["schema_version"])
	}
}

func TestDoctor_CleanWhenNoLinks(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := newCmd()
	if err := runDoctor(context.Background(), cmd, false, false); err != nil {
		t.Fatalf("doctor clean: %v", err)
	}
}

func TestDoctor_BrokenLinkIsError(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	if err := runLink(context.Background(), newCmd(), linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err != nil {
		t.Fatal(err)
	}
	// Remove the workitem directory entirely; the link is now orphaned.
	if err := os.RemoveAll(filepath.Join(root, "workflow", "design", "example")); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	err := runDoctor(context.Background(), cmd, false, false)
	if err == nil {
		t.Fatal("expected doctor to report error-severity findings")
	}
	if !strings.Contains(stdout.String(), codeBrokenLink) {
		t.Fatalf("expected %s finding in stdout, got %q", codeBrokenLink, stdout.String())
	}
}

func TestDoctor_FixRemovesOrphanLink(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	if err := runLink(context.Background(), newCmd(), linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "workflow", "design", "example")); err != nil {
		t.Fatal(err)
	}

	cmd := newCmd()
	if err := runDoctor(context.Background(), cmd, false, true); err != nil {
		t.Fatalf("doctor --fix should clear after auto-repair: %v", err)
	}
	registry, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Links) != 0 {
		t.Fatalf("expected 0 links after --fix, got %d", len(registry.Links))
	}
}

func TestDoctorFixPrunesOrphanPriority(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	store := priority.NewStore()
	priority.Set(store, testWorkitemKey, priority.High)
	priority.Set(store, "design:workflow/design/stale", priority.Low)
	if err := priority.Save(priority.StorePath(root), store); err != nil {
		t.Fatalf("save priority store: %v", err)
	}

	cmd := newCmd()
	if err := runDoctor(context.Background(), cmd, false, true); err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}

	store, err := priority.Load(priority.StorePath(root))
	if err != nil {
		t.Fatalf("load priority store: %v", err)
	}
	if _, ok := store.ManualPriorities["design:workflow/design/stale"]; ok {
		t.Fatal("expected stale priority entry to be pruned")
	}
	if _, ok := store.ManualPriorities[testWorkitemKey]; !ok {
		t.Fatalf("expected valid priority entry %s to remain", testWorkitemKey)
	}
}

func TestDoctor_CurrentMissingIsWarning(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cur := &links.Current{
		Version:    links.CurrentSchemaVersion,
		WorkitemID: "design-gone-2026-05-24",
	}
	if err := links.SaveCurrent(context.Background(), root, cur); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runDoctor(context.Background(), cmd, false, false); err != nil {
		t.Fatalf("warning-only should exit 0: %v", err)
	}
	if !strings.Contains(stdout.String(), codeCurrentMissing) {
		t.Fatalf("expected %s finding, got %q", codeCurrentMissing, stdout.String())
	}
}

func TestDoctor_CurrentKeySelectionIsValid(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	legacyDir := filepath.Join(root, "workflow", "design", "legacy")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "README.md"), []byte("# Legacy\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := links.SaveCurrent(context.Background(), root, &links.Current{
		Version:    links.CurrentSchemaVersion,
		WorkitemID: "design:workflow/design/legacy",
	}); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runDoctor(context.Background(), cmd, false, false); err != nil {
		t.Fatalf("key-based current selection should be valid: %v\n%s", err, stdout.String())
	}
	if strings.Contains(stdout.String(), codeCurrentMissing) {
		t.Fatalf("did not expect %s finding, got %q", codeCurrentMissing, stdout.String())
	}
}

func TestDoctor_MalformedCurrentIsFixableSchemaFinding(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	currentDir := filepath.Join(root, ".campaign", "workitems")
	if err := os.MkdirAll(currentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	currentPath := filepath.Join(currentDir, "current.yaml")
	if err := os.WriteFile(currentPath, []byte("version: not-supported\nworkitem_id: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	err := runDoctor(context.Background(), cmd, false, false)
	if err == nil {
		t.Fatal("expected malformed current.yaml to produce error finding")
	}
	if !strings.Contains(stdout.String(), codeSchemaViolation) ||
		!strings.Contains(stdout.String(), "current.yaml") {
		t.Fatalf("expected current.yaml schema finding, got %q", stdout.String())
	}

	if err := runDoctor(context.Background(), newCmd(), false, true); err != nil {
		t.Fatalf("doctor --fix should clear malformed current.yaml: %v", err)
	}
	if _, err := os.Stat(currentPath); !os.IsNotExist(err) {
		t.Fatalf("current.yaml should be removed by --fix, stat err=%v", err)
	}
}

func TestDoctor_JSONShape(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	if err := runLink(context.Background(), newCmd(), linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "workflow", "design", "example")); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	_ = runDoctor(context.Background(), cmd, true, false)
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, stdout.String())
	}
	if int(payload["error_count"].(float64)) < 1 {
		t.Fatalf("error_count = %v, want >= 1", payload["error_count"])
	}
}
