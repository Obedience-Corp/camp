package crawl

import (
	"errors"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestOptions_DefaultStatusesAndSort(t *testing.T) {
	o := Options{}
	if err := o.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := o.Sort; got != SortStale {
		t.Errorf("Sort = %q, want %q", got, string(SortStale))
	}
	if len(o.Statuses) != 3 {
		t.Fatalf("Statuses len = %d, want 3", len(o.Statuses))
	}
	want := []intent.Status{intent.StatusInbox, intent.StatusReady, intent.StatusActive}
	for i, s := range want {
		if o.Statuses[i] != s {
			t.Errorf("Statuses[%d] = %q, want %q", i, o.Statuses[i], s)
		}
	}
}

func TestOptions_RejectsNegativeLimit(t *testing.T) {
	o := Options{Limit: -1}
	if err := o.Validate(); err == nil {
		t.Fatal("Validate() with negative limit should fail")
	} else if !errors.Is(err, camperrors.ErrInvalidInput) {
		t.Errorf("err = %v, want ErrInvalidInput", err)
	}
}

func TestOptions_RejectsUnknownSort(t *testing.T) {
	o := Options{Sort: "foo"}
	if err := o.Validate(); err == nil {
		t.Fatal("Validate() with unknown sort should fail")
	}
}

func TestOptions_AcceptsAllSortModes(t *testing.T) {
	for _, s := range []SortMode{SortStale, SortUpdated, SortCreated, SortPriority, SortTitle} {
		o := Options{Sort: s}
		if err := o.Validate(); err != nil {
			t.Errorf("sort %q rejected: %v", s, err)
		}
	}
}

func TestOptions_RejectsDungeonStatuses(t *testing.T) {
	for _, s := range []intent.Status{
		intent.StatusDone, intent.StatusKilled, intent.StatusArchived, intent.StatusSomeday,
	} {
		o := Options{Statuses: []intent.Status{s}}
		if err := o.Validate(); err == nil {
			t.Errorf("Validate() with dungeon status %q should fail", s)
		}
	}
}

func TestOptions_AcceptsExplicitLiveStatuses(t *testing.T) {
	o := Options{Statuses: []intent.Status{intent.StatusInbox, intent.StatusReady}}
	if err := o.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(o.Statuses) != 2 {
		t.Errorf("Statuses len = %d, want 2 (no defaulting when explicit)", len(o.Statuses))
	}
}

func TestParseStatusFlag_LiveAccepted(t *testing.T) {
	cases := map[string]intent.Status{
		"inbox":   intent.StatusInbox,
		"INBOX":   intent.StatusInbox,
		"ready":   intent.StatusReady,
		"active":  intent.StatusActive,
		" ready ": intent.StatusReady,
	}
	for raw, want := range cases {
		got, err := ParseStatusFlag(raw)
		if err != nil {
			t.Errorf("ParseStatusFlag(%q) error = %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("ParseStatusFlag(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestParseStatusFlag_DungeonRejected(t *testing.T) {
	for _, raw := range []string{
		"done", "killed", "archived", "someday",
		"dungeon/done", "dungeon/killed", "DUNGEON/ARCHIVED",
	} {
		if _, err := ParseStatusFlag(raw); err == nil {
			t.Errorf("ParseStatusFlag(%q) expected error", raw)
		}
	}
}

func TestParseStatusFlag_UnknownRejected(t *testing.T) {
	if _, err := ParseStatusFlag("nonsense"); err == nil {
		t.Fatal("ParseStatusFlag(nonsense) expected error")
	}
}

func TestFormatNoCandidates(t *testing.T) {
	got := FormatNoCandidates([]intent.Status{intent.StatusInbox, intent.StatusReady})
	want := "No intents to crawl for statuses: inbox, ready."
	if got != want {
		t.Errorf("FormatNoCandidates = %q, want %q", got, want)
	}
}
