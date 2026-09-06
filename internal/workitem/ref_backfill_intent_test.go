//go:build container_fs

package workitem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeIntentFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	writeFile(t, filepath.Join(root, relPath), content)
}

func TestBackfillIntentRef_InsertsAfterID(t *testing.T) {
	root := t.TempDir()
	rel := ".campaign/intents/inbox/test-intent.md"
	original := strings.Join([]string{
		"---",
		"id: test-20260101",
		"title: Test Intent",
		"status: inbox",
		"created_at: 2026-01-01",
		"type: feature",
		"---",
		"",
		"# Test Intent",
		"",
		"Body text.",
	}, "\n")
	writeIntentFile(t, root, rel, original)

	ref := "WI-abc123"
	if err := BackfillIntentRef(context.Background(), root, rel, ref); err != nil {
		t.Fatalf("BackfillIntentRef() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	want := strings.Join([]string{
		"---",
		"id: test-20260101",
		"ref: WI-abc123",
		"title: Test Intent",
		"status: inbox",
		"created_at: 2026-01-01",
		"type: feature",
		"---",
		"",
		"# Test Intent",
		"",
		"Body text.",
	}, "\n")
	if string(got) != want {
		t.Fatalf("backfilled intent file:\n%s\nwant:\n%s", got, want)
	}
}

func TestBackfillIntentRef_NoOpWhenRefSet(t *testing.T) {
	root := t.TempDir()
	rel := ".campaign/intents/active/has-ref.md"
	original := strings.Join([]string{
		"---",
		"id: has-ref-20260101",
		"ref: WI-existing",
		"title: Has Ref",
		"status: active",
		"created_at: 2026-01-01",
		"---",
		"",
		"Body.",
	}, "\n")
	path := filepath.Join(root, rel)
	writeIntentFile(t, root, rel, original)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if err := BackfillIntentRef(context.Background(), root, rel, "WI-new"); err != nil {
		t.Fatalf("BackfillIntentRef() error = %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("file changed when ref already set:\n%s", after)
	}
}

func TestBackfillIntentRef_MalformedFrontmatterErrors(t *testing.T) {
	root := t.TempDir()
	rel := ".campaign/intents/inbox/broken.md"
	writeIntentFile(t, root, rel, "not frontmatter\n\n# Broken\n")

	if err := BackfillIntentRef(context.Background(), root, rel, "WI-abc123"); err == nil {
		t.Fatal("expected error for malformed frontmatter, got nil")
	}
}

func TestDiscoverIntents_SurfacesRefInMetadata(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	writeFile(t, filepath.Join(root, ".campaign/intents/inbox/with-ref.md"),
		"---\nid: with-ref-20260101\nref: WI-deadbeef\ntitle: With Ref\nstatus: inbox\ncreated_at: 2026-01-01\n---\n\nBody.")
	writeFile(t, filepath.Join(root, ".campaign/intents/inbox/no-ref.md"),
		"---\nid: no-ref-20260101\ntitle: No Ref\nstatus: inbox\ncreated_at: 2026-01-01\n---\n\nBody.")

	items, err := discoverIntents(context.Background(), root, resolver)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]WorkItem{}
	for _, item := range items {
		byID[item.SourceID] = item
	}
	withRef := byID["with-ref-20260101"]
	if got, ok := withRef.SourceMetadata["ref"].(string); !ok || got != "WI-deadbeef" {
		t.Fatalf("with-ref metadata ref = %v, want WI-deadbeef", withRef.SourceMetadata["ref"])
	}
	noRef := byID["no-ref-20260101"]
	if _, ok := noRef.SourceMetadata["ref"]; ok {
		t.Fatalf("no-ref metadata should omit ref, got %v", noRef.SourceMetadata["ref"])
	}
}
