package workitem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordPromotion(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("workflow", "feature", "shiny")
	dir := filepath.Join(root, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	original := "version: v1alpha6\nkind: workitem\nid: feature-shiny-x\ntype: feature\ntitle: Shiny\nref: WI-abc123\n"
	if err := os.WriteFile(filepath.Join(dir, MetadataFilename), []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	at := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	if err := RecordPromotion(context.Background(), root, rel, "festivals/planning/shiny-x", at); err != nil {
		t.Fatalf("RecordPromotion() error = %v", err)
	}

	got, err := LoadMetadata(context.Background(), dir)
	if err != nil {
		t.Fatalf("LoadMetadata() error = %v", err)
	}
	if got.PromotedTo != "festivals/planning/shiny-x" {
		t.Fatalf("PromotedTo = %q, want %q", got.PromotedTo, "festivals/planning/shiny-x")
	}
	if got.PromotedAt != "2026-06-18T12:00:00Z" {
		t.Fatalf("PromotedAt = %q, want %q", got.PromotedAt, "2026-06-18T12:00:00Z")
	}
	if got.Ref != "WI-abc123" {
		t.Fatalf("Ref = %q, want preserved", got.Ref)
	}

	raw, err := os.ReadFile(filepath.Join(dir, MetadataFilename))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(raw), "promoted_to: festivals/planning/shiny-x") {
		t.Fatalf("on-disk metadata missing promoted_to:\n%s", raw)
	}
}
