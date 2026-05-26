package resolver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeMinimalCampaign(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `version: campaign/v1
id: testcampaign
name: test
type: product
`
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_MalformedRegistryReturnsHardError(t *testing.T) {
	// Reviewer (CW0003 seq-02 re-review): a malformed links.yaml must surface
	// as an operational failure, not get downgraded to a trace entry that
	// allows a lower-priority tier to pick a wrong workitem.
	root := t.TempDir()
	writeMinimalCampaign(t, root)

	linksDir := filepath.Join(root, ".campaign", "workitems")
	if err := os.MkdirAll(linksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	malformed := "version: workitem-links/v1alpha1\nlinks: [unterminated\n"
	if err := os.WriteFile(filepath.Join(linksDir, "links.yaml"), []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(context.Background(), root, Options{Cwd: root})
	if err == nil {
		t.Fatalf("expected hard error for malformed registry, got nil; result=%+v", got)
	}
	if got == nil {
		t.Fatal("Resolve should still return a partial result with trace on error")
	}
	if got.Source != SourceLink {
		t.Errorf("Source should mark the failing tier as link, got %q", got.Source)
	}
	sawError := false
	for _, step := range got.Trace {
		if step.Tier == SourceLink && step.Result == "error" {
			sawError = true
		}
	}
	if !sawError {
		t.Errorf("expected trace step with Tier=link Result=error, got: %+v", got.Trace)
	}
}

func TestResolve_StaleLinkFallsThrough(t *testing.T) {
	// Stale-link is the one and only recoverable tier failure: a primary
	// link references a workitem id that no longer exists on disk. The
	// resolver should record the diagnostic and continue to the next tier.
	root := t.TempDir()
	writeMinimalCampaign(t, root)

	linksDir := filepath.Join(root, ".campaign", "workitems")
	if err := os.MkdirAll(linksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projDir := filepath.Join(root, "projects", "demo")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := `version: workitem-links/v1alpha1
links:
  - id: lnk_20260524_aaaaaa
    workitem_id: design-ghost-2026-05-26
    scope:
      kind: project
      path: projects/demo
    role: primary
    created_at: 2026-05-24T19:00:00Z
    created_by: camp_workitem_link
`
	if err := os.WriteFile(filepath.Join(linksDir, "links.yaml"), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(context.Background(), root, Options{Cwd: projDir})
	if err != nil {
		t.Fatalf("stale link should fall through, got hard error: %v", err)
	}
	if got.Source != SourceNone {
		t.Errorf("Source = %q, want %q (no lower tier should match)", got.Source, SourceNone)
	}
	sawLinkError := false
	for _, step := range got.Trace {
		if step.Tier == SourceLink && step.Result == "error" {
			sawLinkError = true
		}
	}
	if !sawLinkError {
		t.Errorf("expected stale-link trace step Tier=link Result=error, got: %+v", got.Trace)
	}
}

func TestResolve_AllTiersErrorReturnsSourceNone(t *testing.T) {
	root := t.TempDir()
	writeMinimalCampaign(t, root)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(context.Background(), root, Options{
		Cwd:        root,
		FestivalID: "ghost-festival-id",
	})
	if err != nil {
		t.Fatalf("Resolve should not propagate per-tier errors, got: %v", err)
	}
	if got.Source != SourceNone {
		t.Errorf("Source = %q, want %q (nothing should match)", got.Source, SourceNone)
	}
	if got.Workitem != nil {
		t.Errorf("Workitem should be nil when no tier matches, got: %+v", got.Workitem)
	}
}

func TestResolve_ContextCancelledStopsImmediately(t *testing.T) {
	root := t.TempDir()
	writeMinimalCampaign(t, root)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Resolve(ctx, root, Options{Cwd: root})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}
