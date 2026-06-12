package intent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRename_UpdatesTitleAndFilenameKeepsID(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "original title here",
		Type:      TypeIdea,
		Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	originalID := created.ID

	renamed, err := svc.Rename(ctx, originalID, "a much better title")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if renamed.ID != originalID {
		t.Errorf("id changed on rename: %q -> %q", originalID, renamed.ID)
	}
	if renamed.Title != "a much better title" {
		t.Errorf("Title = %q, want %q", renamed.Title, "a much better title")
	}
	base := filepath.Base(renamed.Path)
	if !strings.HasPrefix(base, "a-much-better-title-") {
		t.Errorf("filename = %q, want slug of new title", base)
	}
	if !strings.HasSuffix(base, "20260119-153412.md") {
		t.Errorf("filename = %q, want stable timestamp suffix preserved", base)
	}
	if _, err := os.Stat(created.Path); !os.IsNotExist(err) {
		t.Errorf("old file still present at %q", created.Path)
	}

	// Lookup by the stable id still resolves after rename.
	got, err := svc.Get(ctx, originalID)
	if err != nil {
		t.Fatalf("Get after rename: %v", err)
	}
	if got.Title != "a much better title" {
		t.Errorf("resolved title = %q", got.Title)
	}
}

func TestRename_SurvivesStatusMove(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "fix the thing",
		Type:      TypeBug,
		Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}

	renamed, err := svc.Rename(ctx, created.ID, "a much clearer title")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	renamedBase := filepath.Base(renamed.Path)
	if !strings.HasPrefix(renamedBase, "a-much-clearer-title-") {
		t.Fatalf("rename did not produce expected slug: %q", renamedBase)
	}

	// A normal triage move must NOT revert the renamed slug to <id>.md.
	moved, err := svc.Move(ctx, created.ID, StatusReady)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if got := filepath.Base(moved.Path); got != renamedBase {
		t.Errorf("move reverted renamed basename: got %q, want %q", got, renamedBase)
	}
	if filepath.Dir(moved.Path) != filepath.Join(intentsDir, "ready") {
		t.Errorf("moved dir = %q, want ready", filepath.Dir(moved.Path))
	}
	// Still resolvable by id after the move, with the renamed title.
	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after move: %v", err)
	}
	if got.Title != "a much clearer title" {
		t.Errorf("title = %q after move", got.Title)
	}

	// The same holds for an audited status change via UpdateDirect.
	active := StatusActive
	if _, _, err := svc.UpdateDirect(ctx, created.ID, UpdateOptions{Status: &active}); err != nil {
		t.Fatalf("UpdateDirect status change: %v", err)
	}
	afterUpdate, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after UpdateDirect: %v", err)
	}
	if filepath.Base(afterUpdate.Path) != renamedBase {
		t.Errorf("UpdateDirect reverted renamed basename: got %q, want %q", filepath.Base(afterUpdate.Path), renamedBase)
	}
}

func TestRename_CollisionProducesDistinctFilename(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	// Two intents sharing the same timestamp suffix would collide if renamed to
	// the same slug. Write one occupying the target filename directly.
	suffix := "20260119-153412"
	id := "first-title-" + suffix
	writeRenamedIntent(t, intentsDir, "inbox", id, id, "first title")
	// Occupy the slug the rename will target.
	occupied := filepath.Join(intentsDir, "inbox", "shared-slug-"+suffix+".md")
	if err := os.WriteFile(occupied, []byte("---\nid: other-"+suffix+"\ntitle: other\nstatus: inbox\ncreated_at: 2026-01-01T00:00:00Z\n---\n"), 0644); err != nil {
		t.Fatalf("write occupied: %v", err)
	}

	renamed, err := svc.Rename(ctx, id, "shared slug")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	base := filepath.Base(renamed.Path)
	if base == "shared-slug-"+suffix+".md" {
		t.Errorf("rename collided onto occupied file: %q", base)
	}
	if !strings.HasPrefix(base, "shared-slug-"+suffix) {
		t.Errorf("filename = %q, want disambiguated shared-slug base", base)
	}
	if _, err := os.Stat(occupied); err != nil {
		t.Errorf("occupied file should be untouched: %v", err)
	}
}

func TestRename_EmptyTitleRejected(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "keep me",
		Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	if _, err := svc.Rename(ctx, created.ID, "   "); err == nil {
		t.Error("Rename with empty title should error")
	}
}

func TestRename_InvalidTitleRejectedWithoutMutating(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	intentsDir := filepath.Join(tmp, "intents")
	svc := NewIntentService(tmp, intentsDir)

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "keep me valid",
		Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}
	originalBase := filepath.Base(created.Path)

	// "xy" is non-empty but fails Intent.Validate (ErrTitleTooShort).
	if _, err := svc.Rename(ctx, created.ID, "xy"); err == nil {
		t.Fatal("Rename to a too-short title should error")
	}

	got, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get after rejected rename: %v", err)
	}
	if got.Title != "keep me valid" {
		t.Errorf("title mutated by rejected rename: %q", got.Title)
	}
	if filepath.Base(got.Path) != originalBase {
		t.Errorf("file moved by rejected rename: %q -> %q", originalBase, filepath.Base(got.Path))
	}
}

// sharedBasenameAcrossStatuses returns two distinct intents that, after both are
// renamed to the same title, share a basename across status dirs: a in inbox and
// b in ready. Rename uniqueness is per-directory, so this collision is legitimate
// (same timestamp suffix, same new slug, different ids) and a move of a into
// ready must not overwrite b.
func sharedBasenameAcrossStatuses(t *testing.T, ctx context.Context) (svc *IntentService, a, b *Intent, intentsDir string) {
	t.Helper()
	tmp := t.TempDir()
	intentsDir = filepath.Join(tmp, "intents")
	svc = NewIntentService(tmp, intentsDir)
	ts := time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC)

	ca, err := svc.CreateDirect(ctx, CreateOptions{Title: "alpha original", Type: TypeIdea, Timestamp: ts})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	cb, err := svc.CreateDirect(ctx, CreateOptions{Title: "beta original", Type: TypeIdea, Timestamp: ts})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if ca.ID == cb.ID {
		t.Fatalf("precondition: ids must differ, got %q", ca.ID)
	}

	if _, err := svc.Move(ctx, cb.ID, StatusReady); err != nil {
		t.Fatalf("park b in ready: %v", err)
	}
	a, err = svc.Rename(ctx, ca.ID, "shared title")
	if err != nil {
		t.Fatalf("rename a: %v", err)
	}
	b, err = svc.Rename(ctx, cb.ID, "shared title")
	if err != nil {
		t.Fatalf("rename b: %v", err)
	}
	if filepath.Base(a.Path) != filepath.Base(b.Path) {
		t.Fatalf("precondition: basenames should collide, got %q vs %q", filepath.Base(a.Path), filepath.Base(b.Path))
	}
	if filepath.Dir(a.Path) != filepath.Join(intentsDir, "inbox") {
		t.Fatalf("precondition: a should be in inbox, got %q", a.Path)
	}
	return svc, a, b, intentsDir
}

func assertNoCrossStatusClobber(t *testing.T, ctx context.Context, svc *IntentService, a, b *Intent, intentsDir, movedPath, bBase string) {
	t.Helper()

	if filepath.Dir(movedPath) != filepath.Join(intentsDir, "ready") {
		t.Errorf("moved a not in ready: %q", movedPath)
	}
	if filepath.Base(movedPath) == bBase {
		t.Fatalf("a collided onto b's basename %q", bBase)
	}

	gotB, err := svc.Get(ctx, b.ID)
	if err != nil {
		t.Fatalf("b lost after moving a: %v", err)
	}
	if filepath.Base(gotB.Path) != bBase {
		t.Errorf("b basename changed: got %q want %q", filepath.Base(gotB.Path), bBase)
	}
	if gotB.Title != "shared title" {
		t.Errorf("b title corrupted: %q", gotB.Title)
	}

	gotA, err := svc.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("a not resolvable after move: %v", err)
	}
	if gotA.Title != "shared title" {
		t.Errorf("a title = %q, want %q", gotA.Title, "shared title")
	}

	ready := StatusReady
	inReady, err := svc.List(ctx, &ListOptions{Status: &ready})
	if err != nil {
		t.Fatalf("List(ready): %v", err)
	}
	if len(inReady) != 2 {
		t.Errorf("ready holds %d intents, want 2 (a and b distinct)", len(inReady))
	}
}

func TestMove_DoesNotOverwriteCrossStatusBasename(t *testing.T) {
	ctx := context.Background()
	svc, a, b, intentsDir := sharedBasenameAcrossStatuses(t, ctx)
	bBase := filepath.Base(b.Path)

	moved, err := svc.Move(ctx, a.ID, StatusReady)
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	assertNoCrossStatusClobber(t, ctx, svc, a, b, intentsDir, moved.Path, bBase)
}

func TestUpdateDirect_DoesNotOverwriteCrossStatusBasename(t *testing.T) {
	ctx := context.Background()
	svc, a, b, intentsDir := sharedBasenameAcrossStatuses(t, ctx)
	bBase := filepath.Base(b.Path)

	ready := StatusReady
	moved, _, err := svc.UpdateDirect(ctx, a.ID, UpdateOptions{Status: &ready})
	if err != nil {
		t.Fatalf("UpdateDirect: %v", err)
	}
	assertNoCrossStatusClobber(t, ctx, svc, a, b, intentsDir, moved.Path, bBase)
}

func TestEdit_DoesNotOverwriteCrossStatusBasename(t *testing.T) {
	ctx := context.Background()
	svc, a, b, intentsDir := sharedBasenameAcrossStatuses(t, ctx)
	bBase := filepath.Base(b.Path)

	editor := func(_ context.Context, path string) error {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out := strings.Replace(string(raw), "status: inbox", "status: ready", 1)
		return os.WriteFile(path, []byte(out), 0644)
	}
	moved, err := svc.Edit(ctx, a.ID, editor)
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	assertNoCrossStatusClobber(t, ctx, svc, a, b, intentsDir, moved.Path, bBase)
}
