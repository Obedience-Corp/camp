package notice

import (
	"testing"
	"time"
)

func TestDismissalFileIsDismissed(t *testing.T) {
	f := &DismissalFile{Version: 1, Dismissed: map[string]time.Time{
		"artifact-root-never-synced:videos": time.Now(),
	}}

	cases := []struct {
		name string
		id   string
		want bool
	}{
		{name: "empty id is never dismissed", id: "", want: false},
		{name: "unknown id", id: "something-else", want: false},
		{name: "dismissed id", id: "artifact-root-never-synced:videos", want: true},
		{
			// Per signature, not per kind: a different root is a different id.
			name: "same kind, different subject",
			id:   "artifact-root-never-synced:media",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := f.IsDismissed(tc.id); got != tc.want {
				t.Errorf("IsDismissed(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

func TestDismissalFileNilIsSafe(t *testing.T) {
	var f *DismissalFile
	if f.IsDismissed("anything") {
		t.Error("nil DismissalFile reported a dismissal")
	}
}

func TestDismissalFileDismiss(t *testing.T) {
	f := &DismissalFile{Version: 1}
	at := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

	if !f.Dismiss("first", at) {
		t.Fatal("Dismiss() = false for a new id, want true")
	}
	if !f.IsDismissed("first") {
		t.Error("the id should be dismissed after Dismiss()")
	}
	if f.Dismiss("first", at) {
		t.Error("Dismiss() = true for an already-dismissed id, want false")
	}
	if !f.Dismiss("second", at) {
		t.Error("a different id must still be dismissible")
	}
}

// Dismissal timestamps are stored in UTC so a file that travels between
// machines does not appear to shift when read in another zone.
func TestDismissalFileStoresUTC(t *testing.T) {
	f := &DismissalFile{Version: 1}
	zone := time.FixedZone("UTC-7", -7*60*60)
	f.Dismiss("id", time.Date(2026, 7, 27, 12, 0, 0, 0, zone))

	if got := f.Dismissed["id"].Location(); got != time.UTC {
		t.Errorf("stored location = %v, want UTC", got)
	}
}

func TestDismissalFileFilter(t *testing.T) {
	notices := []Notice{
		{ID: "a", Message: "first"},
		{ID: "b", Message: "second"},
		{ID: "c", Message: "third"},
	}

	cases := []struct {
		name      string
		dismissed []string
		wantIDs   []string
	}{
		{name: "nothing dismissed keeps all", dismissed: nil, wantIDs: []string{"a", "b", "c"}},
		{name: "one dismissed", dismissed: []string{"b"}, wantIDs: []string{"a", "c"}},
		{name: "all dismissed", dismissed: []string{"a", "b", "c"}, wantIDs: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &DismissalFile{Version: 1, Dismissed: map[string]time.Time{}}
			for _, id := range tc.dismissed {
				f.Dismiss(id, time.Now())
			}

			got := f.Filter(notices)
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("Filter() kept %d notices, want %d", len(got), len(tc.wantIDs))
			}
			for i, want := range tc.wantIDs {
				if got[i].ID != want {
					t.Errorf("Filter()[%d].ID = %q, want %q", i, got[i].ID, want)
				}
			}
		})
	}
}
