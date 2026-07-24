package quest

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func setupQuestCampaign(t *testing.T) (context.Context, string, *Service) {
	t.Helper()

	ctx := context.Background()
	root := t.TempDir()

	if _, err := EnsureScaffold(ctx, root); err != nil {
		t.Fatalf("EnsureScaffold() error = %v", err)
	}

	return ctx, root, NewService(root)
}

// mustDungeonStatusDir resolves the quest dungeon bucket for a status, failing
// the test on the (unexpected) spelling-resolution error.
func mustDungeonStatusDir(t *testing.T, ctx context.Context, root string, status Status) string {
	t.Helper()
	dir, err := DungeonStatusDir(ctx, root, status)
	if err != nil {
		t.Fatalf("DungeonStatusDir(%s) error = %v", status, err)
	}
	return dir
}

func TestListSkipsCorruptQuest(t *testing.T) {
	ctx, root, svc := setupQuestCampaign(t)

	created, err := svc.Create(ctx, "Corruption Survivor", "", "", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := svc.Complete(ctx, created.Quest.ID); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	corruptRoot := QuestDir(root, "corrupt-root")
	if err := os.MkdirAll(corruptRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(QuestPathForDir(corruptRoot), []byte(":\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	corruptDungeon := filepath.Join(mustDungeonStatusDir(t, ctx, root, StatusCompleted), "corrupt-dungeon")
	if err := os.MkdirAll(corruptDungeon, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(QuestPathForDir(corruptDungeon), []byte(":\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var quests []*Quest
	warnings := captureStderr(t, func() {
		var listErr error
		quests, listErr = List(ctx, root, true)
		if listErr != nil {
			t.Fatalf("List() error = %v", listErr)
		}
	})

	if len(quests) != 2 {
		t.Fatalf("List() returned %d quest(s), want default plus completed survivor: %#v", len(quests), quests)
	}
	if !strings.Contains(warnings, `warning: skipping unreadable quest "corrupt-root"`) {
		t.Fatalf("missing root corrupt warning:\n%s", warnings)
	}
	if !strings.Contains(warnings, `warning: skipping unreadable quest "completed/corrupt-dungeon"`) {
		t.Fatalf("missing dungeon corrupt warning:\n%s", warnings)
	}
	for _, q := range quests {
		if q.Slug == "corrupt-root" || q.Slug == "corrupt-dungeon" {
			t.Fatalf("corrupt quest leaked into List result: %#v", q)
		}
	}
}

func TestConcurrentSameNameQuestCreate(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)
	fixed := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	withQuestClock(t, fixed)

	start := make(chan struct{})
	results := make(chan *MutationResult, 2)
	errs := make(chan error, 2)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := svc.Create(ctx, "Concurrent Quest", "", "", nil)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("Create() concurrent error = %v", err)
	}
	seenDirs := map[string]bool{}
	for result := range results {
		dir := filepath.Base(filepath.Dir(result.Quest.Path))
		if seenDirs[dir] {
			t.Fatalf("duplicate quest dir claimed: %s", dir)
		}
		seenDirs[dir] = true
	}
	if len(seenDirs) != 2 {
		t.Fatalf("claimed %d distinct quest dir(s), want 2: %v", len(seenDirs), seenDirs)
	}
}

func TestCreateRegeneratesDuplicateQuestID(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)
	fixed := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	withQuestClock(t, fixed)

	calls := 0
	withQuestIDGenerator(t, func(time.Time) (string, error) {
		calls++
		if calls == 1 {
			return DefaultQuestID, nil
		}
		return "qst_20260615_unique", nil
	})

	result, err := svc.Create(ctx, "Unique ID", "", "", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Quest.ID != "qst_20260615_unique" {
		t.Fatalf("quest ID = %q, want regenerated ID", result.Quest.ID)
	}
	if calls != 2 {
		t.Fatalf("GenerateID calls = %d, want 2", calls)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = old
	})

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stderr = old
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func withQuestClock(t *testing.T, now time.Time) {
	t.Helper()
	old := nowUTC
	nowUTC = func() time.Time { return now }
	t.Cleanup(func() {
		nowUTC = old
	})
}

func withQuestIDGenerator(t *testing.T, fn func(time.Time) (string, error)) {
	t.Helper()
	old := generateQuestID
	generateQuestID = fn
	t.Cleanup(func() {
		generateQuestID = old
	})
}

func TestSaveAtomicWriteContent(t *testing.T) {
	ctx, root, _ := setupQuestCampaign(t)
	now := time.Date(2026, 1, 20, 2, 0, 0, 0, time.UTC)
	path := QuestPathForDir(QuestDir(root, "atomic-quest"))
	q := &Quest{
		ID:        "qst_atomic",
		Name:      "Atomic Quest",
		Status:    StatusOpen,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := Save(ctx, path, q); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Contains(got, []byte("Atomic Quest")) {
		t.Fatalf("saved quest content missing name:\n%s", got)
	}
}

func TestSaveAtomicFailurePreservesOriginal(t *testing.T) {
	ctx, root, _ := setupQuestCampaign(t)
	now := time.Date(2026, 1, 20, 2, 15, 0, 0, time.UTC)
	path := QuestPathForDir(QuestDir(root, "preserve-quest"))
	q := &Quest{
		ID:        "qst_preserve",
		Name:      "Original Quest",
		Status:    StatusOpen,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := Save(ctx, path, q); err != nil {
		t.Fatalf("Save(original) error = %v", err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(original) error = %v", err)
	}

	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0555); err != nil {
		t.Skipf("chmod read-only directory: %v", err)
	}
	defer func() {
		_ = os.Chmod(dir, 0o755)
	}()

	q.Name = "Mutated Quest"
	err = Save(ctx, path, q)
	if err == nil {
		_ = os.Chmod(dir, 0o755)
		_ = os.WriteFile(path, original, 0o644)
		t.Skip("read-only directory did not prevent atomic temp file creation")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(after failed Save) error = %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("failed Save changed original content:\n got: %q\nwant: %q", got, original)
	}
}

func TestServiceCreatePauseResumeCompleteRestore(t *testing.T) {
	ctx, root, svc := setupQuestCampaign(t)

	created, err := svc.Create(ctx, "Runtime Hardening", "Stabilize the runtime", "Refine retry logic", []string{"runtime", "stability"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Quest == nil {
		t.Fatal("Create() returned nil quest")
	}
	if created.Quest.Status != StatusOpen {
		t.Fatalf("created quest status = %q, want %q", created.Quest.Status, StatusOpen)
	}
	if created.Quest.Slug == "" {
		t.Fatal("created quest slug should be set")
	}

	paused, err := svc.Pause(ctx, created.Quest.ID)
	if err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if paused.Quest.Status != StatusPaused {
		t.Fatalf("paused quest status = %q, want %q", paused.Quest.Status, StatusPaused)
	}

	resumed, err := svc.Resume(ctx, created.Quest.ID)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if resumed.Quest.Status != StatusOpen {
		t.Fatalf("resumed quest status = %q, want %q", resumed.Quest.Status, StatusOpen)
	}

	completed, err := svc.Complete(ctx, created.Quest.ID)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completed.Quest.Status != StatusCompleted {
		t.Fatalf("completed quest status = %q, want %q", completed.Quest.Status, StatusCompleted)
	}
	if got := filepath.Dir(completed.Quest.Path); got != filepath.Join(mustDungeonStatusDir(t, ctx, root, StatusCompleted), completed.Quest.Slug) {
		t.Fatalf("completed quest directory = %q", got)
	}

	restored, err := svc.Restore(ctx, created.Quest.ID)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if restored.Quest.Status != StatusOpen {
		t.Fatalf("restored quest status = %q, want %q", restored.Quest.Status, StatusOpen)
	}
	if got := filepath.Dir(restored.Quest.Path); got != filepath.Join(QuestsDir(root), restored.Quest.Slug) {
		t.Fatalf("restored quest directory = %q", got)
	}

	// Verify quest is findable (no .active file needed — multiple quests can be open)
	found, err := svc.Find(ctx, created.Quest.ID)
	if err != nil {
		t.Fatalf("Find() after restore error = %v", err)
	}
	if found.Status != StatusOpen {
		t.Fatalf("restored quest status via Find = %q, want %q", found.Status, StatusOpen)
	}

	_ = root // root used by DungeonStatusDir/QuestsDir above
}

func TestServiceEditAndList(t *testing.T) {
	ctx, root, svc := setupQuestCampaign(t)

	first, err := svc.Create(ctx, "Alpha Quest", "Alpha purpose", "Alpha description", []string{"alpha"})
	if err != nil {
		t.Fatalf("Create(alpha) error = %v", err)
	}
	second, err := svc.Create(ctx, "Beta Quest", "Beta purpose", "Beta description", []string{"beta"})
	if err != nil {
		t.Fatalf("Create(beta) error = %v", err)
	}

	edited, err := svc.Edit(ctx, second.Quest.ID, func(_ context.Context, path string) error {
		q, err := Load(ctx, path)
		if err != nil {
			return err
		}
		q.Name = "Beta Quest Updated"
		q.Description = "Updated description"
		q.Tags = []string{"beta", "release"}
		return Save(ctx, path, q)
	})
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if edited.Quest.Name != "Beta Quest Updated" {
		t.Fatalf("edited quest name = %q", edited.Quest.Name)
	}

	if _, err := svc.Complete(ctx, first.Quest.ID); err != nil {
		t.Fatalf("Complete(alpha) error = %v", err)
	}

	activeOnly, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List(nil) error = %v", err)
	}
	if len(activeOnly) != 2 {
		t.Fatalf("List(nil) length = %d, want 2", len(activeOnly))
	}

	all, err := svc.List(ctx, &ListOptions{All: true})
	if err != nil {
		t.Fatalf("List(all) error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List(all) length = %d, want 3", len(all))
	}

	dungeonOnly, err := svc.List(ctx, &ListOptions{Dungeon: true})
	if err != nil {
		t.Fatalf("List(dungeon) error = %v", err)
	}
	if len(dungeonOnly) != 1 || dungeonOnly[0].ID != first.Quest.ID {
		t.Fatalf("List(dungeon) = %#v, want only completed alpha quest", dungeonOnly)
	}

	found, err := svc.Find(ctx, "Beta Quest Updated")
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if found.ID != second.Quest.ID {
		t.Fatalf("Find() id = %q, want %q", found.ID, second.Quest.ID)
	}

	// ResolveContext with no flag and no env var returns nil (no implicit active quest).
	noContext, err := ResolveContext(ctx, root, "")
	if err != nil {
		t.Fatalf("ResolveContext(empty) error = %v", err)
	}
	if noContext != nil {
		t.Fatalf("ResolveContext(empty) = %#v, want nil (no implicit active quest)", noContext)
	}

	// ResolveContext with explicit ID resolves the correct quest.
	explicit, err := ResolveContext(ctx, root, second.Quest.ID)
	if err != nil {
		t.Fatalf("ResolveContext(explicit) error = %v", err)
	}
	if explicit == nil || explicit.ID != second.Quest.ID {
		t.Fatalf("ResolveContext(explicit) id = %#v, want %q", explicit, second.Quest.ID)
	}
}

func TestServiceDefaultQuestIsMutable(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	// The default quest is a normal quest: it can be renamed and have its
	// lifecycle changed like any other. It exists only to guarantee a campaign
	// always has a quest, not to be read-only.
	if _, err := svc.Rename(ctx, DefaultQuestID, "My Workspace"); err != nil {
		t.Fatalf("Rename(default) unexpected error: %v", err)
	}
	if _, err := svc.Pause(ctx, DefaultQuestID); err != nil {
		t.Fatalf("Pause(default) unexpected error: %v", err)
	}
}

func TestEnsureScaffoldDoesNotDuplicateCompletedDefault(t *testing.T) {
	ctx, root, svc := setupQuestCampaign(t)

	if _, err := svc.Complete(ctx, DefaultQuestID); err != nil {
		t.Fatalf("Complete(default) error = %v", err)
	}

	// quest list re-runs EnsureScaffold; it must not mint a second quest sharing
	// the fixed default identity while the completed default sits in the dungeon.
	if _, err := EnsureScaffold(ctx, root); err != nil {
		t.Fatalf("EnsureScaffold() error = %v", err)
	}

	all, err := List(ctx, root, true)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	var defaults []*Quest
	for _, q := range all {
		if q.IsDefault() {
			defaults = append(defaults, q)
		}
	}
	if len(defaults) != 1 {
		t.Fatalf("found %d default quests, want 1: %#v", len(defaults), defaults)
	}
	if defaults[0].Status != StatusCompleted {
		t.Fatalf("default status = %s, want %s", defaults[0].Status, StatusCompleted)
	}

	resolved, err := Resolve(ctx, root, DefaultQuestID)
	if err != nil {
		t.Fatalf("Resolve(%s) error = %v", DefaultQuestID, err)
	}
	if resolved.Status != StatusCompleted {
		t.Fatalf("resolved default status = %s, want %s", resolved.Status, StatusCompleted)
	}

	restored, err := svc.Restore(ctx, DefaultQuestID)
	if err != nil {
		t.Fatalf("Restore(%s) error = %v", DefaultQuestID, err)
	}
	if restored.Quest.Status != StatusOpen {
		t.Fatalf("restored default status = %s, want %s", restored.Quest.Status, StatusOpen)
	}
}

func TestEnsureScaffoldMigrationPaths(t *testing.T) {
	t.Run("legacy flat file migrated to directory", func(t *testing.T) {
		ctx := context.Background()
		root := t.TempDir()

		// Create quests dir with legacy default.yaml (old layout).
		questsDir := filepath.Join(root, RootDirName)
		if err := os.MkdirAll(questsDir, 0755); err != nil {
			t.Fatal(err)
		}
		legacyPath := filepath.Join(questsDir, "default.yaml")
		q := DefaultQuest(time.Now().UTC())
		if err := writeQuestFile(legacyPath, q); err != nil {
			t.Fatal(err)
		}

		result, err := EnsureScaffold(ctx, root)
		if err != nil {
			t.Fatalf("EnsureScaffold() error = %v", err)
		}

		// New path should exist.
		newPath := DefaultPath(root)
		if _, err := os.Stat(newPath); err != nil {
			t.Fatalf("new default quest not found at %s: %v", newPath, err)
		}

		// Legacy path should be gone.
		if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
			t.Fatalf("legacy default.yaml should have been removed, got err = %v", err)
		}

		// Result should report the migration.
		found := false
		for _, f := range result.CreatedFiles {
			if f == newPath {
				found = true
				break
			}
		}
		if !found {
			t.Error("migration should report new path in CreatedFiles")
		}
	})

	t.Run("both exist keeps directory version", func(t *testing.T) {
		ctx := context.Background()
		root := t.TempDir()

		questsDir := filepath.Join(root, RootDirName)
		defaultDir := filepath.Join(questsDir, DefaultDirName)
		if err := os.MkdirAll(defaultDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Write both legacy and new.
		q := DefaultQuest(time.Now().UTC())
		legacyPath := filepath.Join(questsDir, "default.yaml")
		if err := writeQuestFile(legacyPath, q); err != nil {
			t.Fatal(err)
		}
		newPath := filepath.Join(defaultDir, FileName)
		if err := writeQuestFile(newPath, q); err != nil {
			t.Fatal(err)
		}

		if _, err := EnsureScaffold(ctx, root); err != nil {
			t.Fatalf("EnsureScaffold() error = %v", err)
		}

		// New path should still exist.
		if _, err := os.Stat(newPath); err != nil {
			t.Fatalf("directory quest.yaml should still exist: %v", err)
		}

		// Legacy should be removed.
		if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
			t.Fatalf("legacy default.yaml should be removed, got err = %v", err)
		}
	})

	t.Run("fresh scaffold creates directory layout", func(t *testing.T) {
		ctx := context.Background()
		root := t.TempDir()

		if _, err := EnsureScaffold(ctx, root); err != nil {
			t.Fatalf("EnsureScaffold() error = %v", err)
		}

		newPath := DefaultPath(root)
		info, err := os.Stat(newPath)
		if err != nil {
			t.Fatalf("default quest not created at %s: %v", newPath, err)
		}
		if info.IsDir() {
			t.Fatal("quest.yaml should be a file, not a directory")
		}

		loaded, err := LoadDefault(ctx, root)
		if err != nil {
			t.Fatalf("LoadDefault() error = %v", err)
		}
		if loaded.Slug != DefaultDirName {
			t.Errorf("slug = %q, want %q", loaded.Slug, DefaultDirName)
		}
	})
}
