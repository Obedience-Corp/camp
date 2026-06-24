package workitem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeCommitContextCampaign(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".campaign", "campaign.yaml"),
		[]byte("id: test-campaign\nname: Test\ntype: product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func seedDesignWorkitemMarker(t *testing.T, root, ref string) string {
	t.Helper()
	wiDir := filepath.Join(root, "workflow", "design", "example")
	if err := os.MkdirAll(wiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := "version: v1alpha6\nkind: workitem\nid: design-example-2026-05-24\ntype: design\ntitle: Example\nref: " + ref + "\n"
	if err := os.WriteFile(filepath.Join(wiDir, ".workitem"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	return wiDir
}

func TestResolveCommitContext_InheritsAncestorWorkitemRef(t *testing.T) {
	const ref = "WI-abc123"
	root := writeCommitContextCampaign(t)
	wiDir := seedDesignWorkitemMarker(t, root, ref)

	var errw bytes.Buffer
	cc := ResolveCommitContext(context.Background(), root, wiDir, &errw)

	if cc.WorkitemRef != ref {
		t.Errorf("WorkitemRef = %q, want %q (errw=%q)", cc.WorkitemRef, ref, errw.String())
	}
	if cc.FestivalRef != "" {
		t.Errorf("FestivalRef = %q, want empty for a non-festival source", cc.FestivalRef)
	}
	if cc.QuestID != "" {
		t.Errorf("QuestID = %q, want empty", cc.QuestID)
	}
}

func TestResolveCommitContext_NoContextIsZero(t *testing.T) {
	root := writeCommitContextCampaign(t)

	cc := ResolveCommitContext(context.Background(), root, root, nil)

	if cc != (CommitContext{}) {
		t.Errorf("expected zero CommitContext when no workitem resolves, got %+v", cc)
	}
}

func TestResolveCommitContext_CancelledContextIsZero(t *testing.T) {
	const ref = "WI-abc123"
	root := writeCommitContextCampaign(t)
	wiDir := seedDesignWorkitemMarker(t, root, ref)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cc := ResolveCommitContext(ctx, root, wiDir, nil)
	if cc != (CommitContext{}) {
		t.Errorf("expected zero CommitContext on cancelled context, got %+v", cc)
	}
}
