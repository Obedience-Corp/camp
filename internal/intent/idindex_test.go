package intent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeRenamedIntent writes an intent file whose on-disk filename slug differs
// from its id: frontmatter, simulating the post-rename state.
func writeRenamedIntent(t *testing.T, intentsDir, status, filenameSlug, id, title string) string {
	t.Helper()
	dir := filepath.Join(intentsDir, status)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := "---\n" +
		"id: " + id + "\n" +
		"title: " + title + "\n" +
		"status: " + status + "\n" +
		"created_at: 2026-01-01T00:00:00Z\n" +
		"type: idea\n" +
		"---\n\n# " + title + "\n"
	path := filepath.Join(dir, filenameSlug+".md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestResolveByID_FilenameSlugDiffersFromID(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	id := "original-title-20260101-000000"
	wantPath := writeRenamedIntent(t, intentsDir, "inbox", "a-renamed-slug-20260101-000000", id, "A renamed slug")

	// Get resolves by id: even though the filename slug differs.
	got, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get(%q): %v", id, err)
	}
	if got.ID != id {
		t.Errorf("resolved ID = %q, want %q", got.ID, id)
	}
	if got.Path != wantPath {
		t.Errorf("resolved path = %q, want %q", got.Path, wantPath)
	}
}

func TestMove_ResolvesRenamedFileByID(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	id := "do-the-thing-20260101-000000"
	writeRenamedIntent(t, intentsDir, "inbox", "renamed-thing-20260101-000000", id, "Renamed thing")

	moved, err := svc.Move(ctx, id, StatusReady)
	if err != nil {
		t.Fatalf("Move(%q): %v", id, err)
	}
	if moved.Status != StatusReady {
		t.Errorf("Status = %q, want ready", moved.Status)
	}
	// The intent is now resolvable in its new status.
	if _, err := svc.Get(ctx, id); err != nil {
		t.Errorf("Get after move: %v", err)
	}
}

func TestParser_TrustsFrontmatterID(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	id := "trusted-id-20260101-000000"
	writeRenamedIntent(t, intentsDir, "inbox", "totally-different-filename", id, "Trust the frontmatter")

	got, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != id {
		t.Errorf("ID = %q, want %q (must come from frontmatter, not filename)", got.ID, id)
	}
}
