package workitem

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

	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
)

// linkTestCampaign builds a minimal campaign with one design workitem at
// workflow/design/example/.workitem so the selector resolver can find it.
func linkTestCampaign(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
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
	const meta = `version: v1alpha5
kind: workitem
id: design-example-2026-05-24
type: design
title: Example
`
	if err := os.WriteFile(filepath.Join(wiDir, ".workitem"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "projects", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}
}

func newCmd() *cobra.Command {
	c := &cobra.Command{}
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	return c
}

func TestLink_HappyPath(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := newCmd()
	if err := runLink(context.Background(), cmd, linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err != nil {
		t.Fatalf("runLink: %v", err)
	}

	registry, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(registry.Links))
	}
	link := registry.Links[0]
	if link.WorkitemID != "design-example-2026-05-24" {
		t.Fatalf("workitem_id = %q", link.WorkitemID)
	}
	if link.Scope.Kind != links.ScopeProject || link.Scope.Path != "projects/demo" {
		t.Fatalf("scope = %#v", link.Scope)
	}
	if link.Role != links.RolePrimary {
		t.Fatalf("role = %s", link.Role)
	}
	if link.CreatedBy == "" {
		t.Fatalf("created_by must not be empty")
	}
}

func TestSanitizeCreatedBy(t *testing.T) {
	if got := sanitizeCreatedBy("DOMAIN\\Lance Rogers!"); got != "DOMAIN-Lance-Rogers" {
		t.Fatalf("sanitizeCreatedBy = %q", got)
	}
	long := strings.Repeat("a", 70)
	if got := sanitizeCreatedBy(long); len(got) != 64 {
		t.Fatalf("sanitizeCreatedBy length = %d, want 64", len(got))
	}
}

func TestLink_MissingSelectorErrors(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := newCmd()
	err := runLink(context.Background(), cmd, linkOptions{
		Selector: "no-such-workitem",
		Project:  "demo",
	})
	if err == nil {
		t.Fatal("expected error for missing selector")
	}
	if !strings.Contains(err.Error(), "no workitem matched") {
		t.Fatalf("err = %v, expected 'no workitem matched'", err)
	}
	if !errors.Is(err, selector.ErrSelectorNotFound) {
		t.Fatalf("err = %v, want ErrSelectorNotFound", err)
	}
}

func TestLink_AmbiguousSelector(t *testing.T) {
	root := linkTestCampaign(t)
	// Add a second design workitem with the same slug to trigger ambiguity on
	// the slug-match path.
	otherDir := filepath.Join(root, "workflow", "design", "duplicate-slug")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const otherMeta = `version: v1alpha5
kind: workitem
id: design-duplicate-2026-05-24
type: design
title: Duplicate
`
	if err := os.WriteFile(filepath.Join(otherDir, ".workitem"), []byte(otherMeta), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := chdir(t, root)
	defer restore()

	// Use a fuzzy-style ambiguous selector that matches slug exactness on
	// neither but matches multiple via key. "design" matches both via key
	// when AllowFuzzy is on — but selector default has fuzzy off, so it
	// should fall to ErrNotFound here. Construct a real ambiguity instead:
	// create two workitems sharing the same slug, then look up by slug.
	thirdDir := filepath.Join(root, "workflow", "design", "shared")
	fourthDir := filepath.Join(root, "workflow", "explore", "shared")
	for _, dir := range []string{thirdDir, fourthDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		meta := "version: v1alpha5\nkind: workitem\nid: " +
			filepath.Base(dir) + "-" + filepath.Base(filepath.Dir(dir)) +
			"-2026-05-24\ntype: " + filepath.Base(filepath.Dir(dir)) + "\n"
		if err := os.WriteFile(filepath.Join(dir, ".workitem"), []byte(meta), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cmd := newCmd()
	err := runLink(context.Background(), cmd, linkOptions{
		Selector: "shared",
		Project:  "demo",
	})
	if err == nil {
		t.Fatal("expected error for ambiguous selector")
	}
	if !strings.Contains(err.Error(), "multiple workitems") {
		t.Fatalf("err = %v, want multiple-match message", err)
	}
	if !errors.Is(err, selector.ErrSelectorAmbiguous) {
		t.Fatalf("err = %v, want ErrSelectorAmbiguous", err)
	}
}

func TestLink_DuplicatePrimaryRequiresReplace(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := newCmd()
	if err := runLink(context.Background(), cmd, linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err != nil {
		t.Fatalf("first link: %v", err)
	}
	if err := runLink(context.Background(), cmd, linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err == nil {
		t.Fatal("expected collision without --replace")
	}
	if err := runLink(context.Background(), cmd, linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
		Replace:  true,
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	registry, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Links) != 1 {
		t.Fatalf("expected 1 link after replace, got %d", len(registry.Links))
	}
}

func TestLink_JSONShape(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := &cobra.Command{}
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runLink(context.Background(), cmd, linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
		JSON:     true,
	}); err != nil {
		t.Fatalf("runLink: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, stdout.String())
	}
	if payload["schema_version"] != links.LinksSchemaVersion {
		t.Fatalf("schema_version = %v", payload["schema_version"])
	}
}

func TestUnlink_RemovesByID(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := newCmd()
	if err := runLink(context.Background(), cmd, linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err != nil {
		t.Fatal(err)
	}
	registry, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	linkID := registry.Links[0].ID

	if err := runUnlink(context.Background(), cmd, unlinkOptions{ID: linkID}); err != nil {
		t.Fatalf("runUnlink: %v", err)
	}
	after, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Links) != 0 {
		t.Fatalf("expected 0 links after unlink, got %d", len(after.Links))
	}
}

func TestUnlink_RoundTripBySelector(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := newCmd()
	if err := runLink(context.Background(), cmd, linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err != nil {
		t.Fatal(err)
	}
	if err := runUnlink(context.Background(), cmd, unlinkOptions{
		Selector:     "design-example-2026-05-24",
		ExplicitPath: "projects/demo",
	}); err != nil {
		t.Fatalf("runUnlink: %v", err)
	}
	after, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Links) != 0 {
		t.Fatalf("expected 0 links after selector-based unlink, got %d", len(after.Links))
	}
}

func TestLinks_ListEmptyAndFiltered(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := newCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})
	if err := runLinks(context.Background(), cmd, "", false); err != nil {
		t.Fatalf("runLinks empty: %v", err)
	}
	if !strings.Contains(stdout.String(), "no links") {
		t.Fatalf("empty output = %q", stdout.String())
	}

	if err := runLink(context.Background(), cmd, linkOptions{
		Selector: "design-example-2026-05-24",
		Project:  "demo",
	}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	cmd.SetOut(&stdout)
	if err := runLinks(context.Background(), cmd, "design-example-2026-05-24", false); err != nil {
		t.Fatalf("runLinks filtered: %v", err)
	}
	if !strings.Contains(stdout.String(), "design-example-2026-05-24") {
		t.Fatalf("filtered output missing workitem id: %q", stdout.String())
	}
}

func TestLinks_FilteredOutputIsSorted(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	for _, dir := range []string{"projects/a", "projects/z"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "workitems"), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := `version: workitem-links/v1alpha1
links:
  - id: lnk_20260524_ffffff
    workitem_id: design-example-2026-05-24
    workitem_key: design:workflow/design/example
    scope:
      kind: project
      path: projects/z
    role: related
    created_at: 2026-05-24T19:00:00Z
    created_by: test
  - id: lnk_20260524_aaaaaa
    workitem_id: design-example-2026-05-24
    workitem_key: design:workflow/design/example
    scope:
      kind: project
      path: projects/a
    role: related
    created_at: 2026-05-24T19:00:00Z
    created_by: test
`
	if err := os.WriteFile(filepath.Join(root, ".campaign", "workitems", "links.yaml"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := runLinks(context.Background(), cmd, "design-example-2026-05-24", true); err != nil {
		t.Fatalf("runLinks filtered json: %v", err)
	}
	var payload struct {
		Links []links.Link `json:"links"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v\nraw=%s", err, stdout.String())
	}
	if len(payload.Links) != 2 {
		t.Fatalf("links len = %d, want 2", len(payload.Links))
	}
	if payload.Links[0].Scope.Path != "projects/a" || payload.Links[1].Scope.Path != "projects/z" {
		t.Fatalf("filtered links not sorted: %#v", payload.Links)
	}
}

func TestCurrent_SetGetClear(t *testing.T) {
	root := linkTestCampaign(t)
	restore := chdir(t, root)
	defer restore()

	cmd := newCmd()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&bytes.Buffer{})

	if err := runCurrent(context.Background(), cmd, "", false, false); err != nil {
		t.Fatalf("runCurrent empty get: %v", err)
	}
	if !strings.Contains(stdout.String(), "no current workitem") {
		t.Fatalf("empty get output = %q", stdout.String())
	}

	stdout.Reset()
	cmd.SetOut(&stdout)
	if err := runCurrent(context.Background(), cmd, "design-example-2026-05-24", false, false); err != nil {
		t.Fatalf("runCurrent set: %v", err)
	}
	if !strings.Contains(stdout.String(), "set current workitem") {
		t.Fatalf("set output = %q", stdout.String())
	}

	stdout.Reset()
	cmd.SetOut(&stdout)
	if err := runCurrent(context.Background(), cmd, "", false, false); err != nil {
		t.Fatalf("runCurrent get: %v", err)
	}
	if !strings.Contains(stdout.String(), "design-example-2026-05-24") {
		t.Fatalf("get output = %q", stdout.String())
	}

	stdout.Reset()
	cmd.SetOut(&stdout)
	if err := runCurrent(context.Background(), cmd, "", true, false); err != nil {
		t.Fatalf("runCurrent clear: %v", err)
	}
	if !strings.Contains(stdout.String(), "cleared") {
		t.Fatalf("clear output = %q", stdout.String())
	}

	// Verify file is gone.
	if _, err := os.Stat(filepath.Join(root, ".campaign", "workitems", "current.yaml")); !os.IsNotExist(err) {
		t.Fatalf("current.yaml should not exist after --clear: err=%v", err)
	}
}

func TestLink_CwdInfersScope(t *testing.T) {
	root := linkTestCampaign(t)
	// Run from inside projects/demo so --cwd picks it up as scope.
	cwd := filepath.Join(root, "projects", "demo")
	restore := chdir(t, cwd)
	defer restore()

	cmd := newCmd()
	if err := runLink(context.Background(), cmd, linkOptions{
		Selector: "design-example-2026-05-24",
		UseCwd:   true,
	}); err != nil {
		t.Fatalf("runLink --cwd: %v", err)
	}
	registry, err := links.Load(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(registry.Links))
	}
	if registry.Links[0].Scope.Kind != links.ScopeProject {
		t.Fatalf("scope.kind = %s, want project", registry.Links[0].Scope.Kind)
	}
	if registry.Links[0].Scope.Path != "projects/demo" {
		t.Fatalf("scope.path = %q", registry.Links[0].Scope.Path)
	}
}
