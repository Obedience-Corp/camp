package quest

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
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
	if got := filepath.Dir(completed.Quest.Path); got != filepath.Join(DungeonStatusDir(root, StatusCompleted), completed.Quest.Slug) {
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

func TestServiceDefaultQuestLifecycleProtected(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	if _, err := svc.Pause(ctx, DefaultQuestID); !errors.Is(err, ErrDefaultQuestReadOnly) {
		t.Fatalf("Pause(default) error = %v, want %v", err, ErrDefaultQuestReadOnly)
	}
}
