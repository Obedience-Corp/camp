package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

func TestWorkitemRefFor_HandlesNilAndEmpty(t *testing.T) {
	if got := workitemRefFor(nil); got != "" {
		t.Fatalf("nil workitem: got %q, want empty", got)
	}
	if got := workitemRefFor(&workitem.WorkItem{}); got != "" {
		t.Fatalf("empty SourceMetadata: got %q, want empty", got)
	}
	wi := &workitem.WorkItem{
		SourceMetadata: map[string]any{"ref": "WI-abcdef"},
	}
	if got := workitemRefFor(wi); got != "WI-abcdef" {
		t.Fatalf("got %q, want WI-abcdef", got)
	}
}

func TestResolveCommitContext_GracefulOutsideCampaign(t *testing.T) {
	tmp := t.TempDir()
	// Set a working directory outside any campaign so the resolver returns
	// (none, none).
	cur, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cur) }()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	questID, ref := resolveCommitContext(context.Background(), filepath.Join(tmp, "no-campaign"), "")
	if questID != "" || ref != "" {
		t.Fatalf("expected empty strings for non-campaign root, got quest=%q ref=%q", questID, ref)
	}
}
