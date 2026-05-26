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

func TestResolve_TierLoadErrorContinues(t *testing.T) {
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
	if err != nil {
		t.Fatalf("Resolve should not propagate per-tier load errors, got: %v", err)
	}
	if got == nil {
		t.Fatal("Resolve returned nil result")
	}
	if got.Source != SourceNone {
		t.Errorf("Source = %q, want %q", got.Source, SourceNone)
	}

	sawError := false
	for _, step := range got.Trace {
		if step.Tier == SourceLink && step.Result == "error" {
			sawError = true
		}
	}
	if !sawError {
		t.Errorf("expected at least one trace step with Tier=link and Result=error, got: %+v", got.Trace)
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
