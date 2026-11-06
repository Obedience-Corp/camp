//go:build container_fs

package quest

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestEnsureDefaultPreservesClosedQuests(t *testing.T) {
	for _, status := range []Status{StatusPaused, StatusCompleted, StatusArchived} {
		t.Run(string(status), func(t *testing.T) {
			ctx, root, svc := setupQuestCampaign(t)
			old, err := LoadDefault(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(filepath.Dir(old.Path), "ui-state.json"), []byte("old workspace"), 0o600); err != nil {
				t.Fatal(err)
			}
			var moved *MutationResult
			switch status {
			case StatusPaused:
				moved, err = svc.Pause(ctx, old.ID)
			case StatusCompleted:
				moved, err = svc.Complete(ctx, old.ID)
			case StatusArchived:
				moved, err = svc.Archive(ctx, old.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(moved.Quest.Path)
			if err != nil {
				t.Fatal(err)
			}
			// A normal quest called default must not become the fallback.
			if _, err := svc.Create(ctx, "default", "unrelated work", "", nil); err != nil {
				t.Fatal(err)
			}
			fresh, created, err := svc.EnsureDefault(ctx)
			if err != nil || !created || fresh.ID == old.ID || !fresh.Default || fresh.Status != StatusOpen {
				t.Fatalf("fresh default: %+v created=%v err=%v", fresh, created, err)
			}
			reused, created, err := svc.EnsureDefault(ctx)
			if err != nil || created || reused.ID != fresh.ID {
				t.Fatalf("default not reused: %+v created=%v err=%v", reused, created, err)
			}
			if _, err := svc.Complete(ctx, fresh.ID); err != nil {
				t.Fatal(err)
			}
			next, created, err := svc.EnsureDefault(ctx)
			if err != nil || !created || next.ID == fresh.ID || next.ID == old.ID {
				t.Fatalf("second renewal: %+v created=%v err=%v", next, created, err)
			}
			after, err := os.ReadFile(moved.Quest.Path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("historical metadata changed: %v", err)
			}
			workspace, err := os.ReadFile(filepath.Join(filepath.Dir(moved.Quest.Path), "ui-state.json"))
			if err != nil || string(workspace) != "old workspace" {
				t.Fatalf("historical workspace changed: %q %v", workspace, err)
			}
		})
	}
}

func TestEnsureDefaultConcurrentAndCancelled(t *testing.T) {
	ctx, root, svc := setupQuestCampaign(t)
	if _, err := svc.Complete(ctx, DefaultQuestID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	type result struct {
		id      string
		created bool
		err     error
	}
	results := make(chan result, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q, created, err := NewService(root).EnsureDefault(ctx)
			id := ""
			if q != nil {
				id = q.ID
			}
			results <- result{id, created, err}
		}()
	}
	wg.Wait()
	close(results)
	id, creations := "", 0
	for got := range results {
		if got.err != nil || got.id == "" {
			t.Fatalf("concurrent default: %+v", got)
		}
		if id != "" && id != got.id {
			t.Fatalf("multiple defaults: %s and %s", id, got.id)
		}
		id = got.id
		if got.created {
			creations++
		}
	}
	if creations != 1 {
		t.Fatalf("created %d defaults, want one", creations)
	}
	if _, err := svc.Complete(ctx, id); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := svc.EnsureDefault(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled default: %v", err)
	}
	quests, err := List(ctx, root, true)
	if err != nil || len(quests) != 2 {
		t.Fatalf("cancel created a quest: %d, %v", len(quests), err)
	}
}
