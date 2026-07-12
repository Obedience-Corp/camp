package quest

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// TestConcurrentChecklistItemAdd_NoLostUpdate exercises the mutation lock: N
// parallel AddChecklistItem calls on the same quest must all persist. Without a
// file lock around the load → mutate → save window, concurrent adds each load
// the same checklist and last-writer-wins silently drops the others.
func TestConcurrentChecklistItemAdd_NoLostUpdate(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	created, err := svc.Create(ctx, "Lock Quest", "", "", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	questID := created.Quest.ID

	const n = 8
	start := make(chan struct{})
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if _, err := svc.AddChecklistItem(ctx, questID, AddChecklistItemOptions{
				Title: fmt.Sprintf("item-%d", i),
			}); err != nil {
				errs <- err
			}
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("AddChecklistItem concurrent error = %v", err)
	}

	_, cl, err := svc.Checklist(ctx, questID)
	if err != nil {
		t.Fatalf("Checklist() error = %v", err)
	}
	if len(cl.Items) != n {
		t.Fatalf("persisted %d checklist item(s), want %d (lost update under concurrent add)", len(cl.Items), n)
	}
}

// TestRankChecklistItem_RejectsNegative guards the operator-facing rank rule:
// negative ranks would sort ahead of the auto-assigned 10, 20, … sequence.
func TestRankChecklistItem_RejectsNegative(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	created, err := svc.Create(ctx, "Rank Quest", "", "", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	added, err := svc.AddChecklistItem(ctx, created.Quest.ID, AddChecklistItemOptions{Title: "item"})
	if err != nil {
		t.Fatalf("AddChecklistItem() error = %v", err)
	}

	_, err = svc.RankChecklistItem(ctx, created.Quest.ID, added.Item.ID, -1)
	if !errors.Is(err, ErrNegativeChecklistRank) {
		t.Fatalf("RankChecklistItem(-1) error = %v, want ErrNegativeChecklistRank", err)
	}
	if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Fatalf("RankChecklistItem(-1) error = %v, want wrapped ErrInvalidInput", err)
	}

	if _, err := svc.RankChecklistItem(ctx, created.Quest.ID, added.Item.ID, 5); err != nil {
		t.Fatalf("RankChecklistItem(5) error = %v", err)
	}
}

func TestRankChecklistItem_ReturnsAffectedItemAfterSort(t *testing.T) {
	ctx, _, svc := setupQuestCampaign(t)

	created, err := svc.Create(ctx, "Sorted Result Quest", "", "", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	first, err := svc.AddChecklistItem(ctx, created.Quest.ID, AddChecklistItemOptions{Title: "first"})
	if err != nil {
		t.Fatalf("AddChecklistItem(first) error = %v", err)
	}
	second, err := svc.AddChecklistItem(ctx, created.Quest.ID, AddChecklistItemOptions{Title: "second"})
	if err != nil {
		t.Fatalf("AddChecklistItem(second) error = %v", err)
	}

	result, err := svc.RankChecklistItem(ctx, created.Quest.ID, first.Item.ID, 30)
	if err != nil {
		t.Fatalf("RankChecklistItem() error = %v", err)
	}
	if result.Item.ID != first.Item.ID {
		t.Fatalf("result item ID = %q, want affected item %q", result.Item.ID, first.Item.ID)
	}
	if result.Item.Rank != 30 {
		t.Fatalf("result item rank = %d, want 30", result.Item.Rank)
	}
	if len(result.Checklist.Items) != 2 || result.Checklist.Items[0].ID != second.Item.ID || result.Checklist.Items[1].ID != first.Item.ID {
		t.Fatalf("sorted checklist IDs = %#v, want [%q, %q]", result.Checklist.Items, second.Item.ID, first.Item.ID)
	}
}
