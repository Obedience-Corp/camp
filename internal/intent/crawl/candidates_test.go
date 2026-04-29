package crawl

import (
	"context"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func mustValidate(t *testing.T, o *Options) {
	t.Helper()
	if err := o.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSelectCandidates_DefaultScopeIncludesLiveOnly(t *testing.T) {
	store := newFakeStore(
		&intent.Intent{ID: "a", Title: "A", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0)},
		&intent.Intent{ID: "b", Title: "B", Status: intent.StatusReady, CreatedAt: time.Unix(200, 0)},
		&intent.Intent{ID: "c", Title: "C", Status: intent.StatusActive, CreatedAt: time.Unix(300, 0)},
		&intent.Intent{ID: "d", Title: "D", Status: intent.StatusArchived, CreatedAt: time.Unix(400, 0)},
		&intent.Intent{ID: "e", Title: "E", Status: intent.StatusDone, CreatedAt: time.Unix(500, 0)},
	)
	opts := Options{}
	mustValidate(t, &opts)

	got, err := SelectCandidates(context.Background(), store, opts)
	if err != nil {
		t.Fatalf("SelectCandidates error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d candidates, want 3 (live only)", len(got))
	}
	for _, in := range got {
		if in.Status.InDungeon() {
			t.Errorf("dungeon intent %q included as candidate", in.ID)
		}
	}
}

func TestSelectCandidates_ExplicitStatusReplacesScope(t *testing.T) {
	store := newFakeStore(
		&intent.Intent{ID: "a", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0)},
		&intent.Intent{ID: "b", Status: intent.StatusReady, CreatedAt: time.Unix(200, 0)},
		&intent.Intent{ID: "c", Status: intent.StatusActive, CreatedAt: time.Unix(300, 0)},
	)
	opts := Options{Statuses: []intent.Status{intent.StatusReady}}
	mustValidate(t, &opts)

	got, err := SelectCandidates(context.Background(), store, opts)
	if err != nil {
		t.Fatalf("SelectCandidates error = %v", err)
	}
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("got %d/%v, want exactly 'b'", len(got), got)
	}
}

func TestSelectCandidates_LimitAppliesAfterSort(t *testing.T) {
	store := newFakeStore(
		&intent.Intent{ID: "old", Title: "old", Status: intent.StatusInbox,
			CreatedAt: time.Unix(100, 0), UpdatedAt: time.Unix(100, 0)},
		&intent.Intent{ID: "mid", Title: "mid", Status: intent.StatusInbox,
			CreatedAt: time.Unix(200, 0), UpdatedAt: time.Unix(200, 0)},
		&intent.Intent{ID: "new", Title: "new", Status: intent.StatusInbox,
			CreatedAt: time.Unix(300, 0), UpdatedAt: time.Unix(300, 0)},
	)
	opts := Options{Limit: 2, Sort: SortStale}
	mustValidate(t, &opts)

	got, err := SelectCandidates(context.Background(), store, opts)
	if err != nil {
		t.Fatalf("SelectCandidates error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "old" || got[1].ID != "mid" {
		t.Fatalf("order = [%s, %s], want [old, mid] (stalest first)", got[0].ID, got[1].ID)
	}
}

func TestSelectCandidates_StaleUsesUpdatedThenCreated(t *testing.T) {
	store := newFakeStore(
		// Updated long ago - should be first
		&intent.Intent{ID: "stale", Title: "stale", Status: intent.StatusInbox,
			CreatedAt: time.Unix(500, 0), UpdatedAt: time.Unix(50, 0)},
		// No updated_at, created mid - falls back to created_at
		&intent.Intent{ID: "mid", Title: "mid", Status: intent.StatusInbox,
			CreatedAt: time.Unix(100, 0)},
		// Recently updated - should be last
		&intent.Intent{ID: "fresh", Title: "fresh", Status: intent.StatusInbox,
			CreatedAt: time.Unix(50, 0), UpdatedAt: time.Unix(900, 0)},
	)
	opts := Options{}
	mustValidate(t, &opts)

	got, err := SelectCandidates(context.Background(), store, opts)
	if err != nil {
		t.Fatalf("SelectCandidates error = %v", err)
	}
	wantIDs := []string{"stale", "mid", "fresh"}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Errorf("idx %d: got %q, want %q (stale order: %v)", i, got[i].ID, want,
				idsOf(got))
		}
	}
}

func TestSelectCandidates_DedupesByID(t *testing.T) {
	in := &intent.Intent{ID: "shared", Title: "shared", Status: intent.StatusInbox,
		CreatedAt: time.Unix(100, 0)}
	store := newFakeStore(in)
	opts := Options{Statuses: []intent.Status{intent.StatusInbox, intent.StatusReady}}
	mustValidate(t, &opts)

	got, err := SelectCandidates(context.Background(), store, opts)
	if err != nil {
		t.Fatalf("SelectCandidates error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
}

func TestSelectCandidates_TitleSort(t *testing.T) {
	store := newFakeStore(
		&intent.Intent{ID: "z", Title: "Zebra", Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0)},
		&intent.Intent{ID: "a", Title: "Apple", Status: intent.StatusInbox, CreatedAt: time.Unix(200, 0)},
		&intent.Intent{ID: "m", Title: "Mango", Status: intent.StatusInbox, CreatedAt: time.Unix(300, 0)},
	)
	opts := Options{Sort: SortTitle}
	mustValidate(t, &opts)

	got, err := SelectCandidates(context.Background(), store, opts)
	if err != nil {
		t.Fatalf("SelectCandidates error = %v", err)
	}
	wantTitles := []string{"Apple", "Mango", "Zebra"}
	for i, want := range wantTitles {
		if got[i].Title != want {
			t.Errorf("idx %d title = %q, want %q", i, got[i].Title, want)
		}
	}
}

func TestSelectCandidates_PrioritySort(t *testing.T) {
	store := newFakeStore(
		&intent.Intent{ID: "low", Title: "low", Priority: intent.PriorityLow, Status: intent.StatusInbox, CreatedAt: time.Unix(100, 0)},
		&intent.Intent{ID: "high", Title: "high", Priority: intent.PriorityHigh, Status: intent.StatusInbox, CreatedAt: time.Unix(200, 0)},
		&intent.Intent{ID: "med", Title: "med", Priority: intent.PriorityMedium, Status: intent.StatusInbox, CreatedAt: time.Unix(300, 0)},
	)
	opts := Options{Sort: SortPriority}
	mustValidate(t, &opts)

	got, err := SelectCandidates(context.Background(), store, opts)
	if err != nil {
		t.Fatalf("SelectCandidates error = %v", err)
	}
	if got[0].ID != "high" {
		t.Errorf("first = %q, want high", got[0].ID)
	}
}

func idsOf(in []*intent.Intent) []string {
	ids := make([]string, len(in))
	for i, x := range in {
		ids[i] = x.ID
	}
	return ids
}
