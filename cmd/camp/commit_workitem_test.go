package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

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
