package notice

import (
	"strings"
	"testing"
	"time"
)

func TestDismissalFileIsDismissed(t *testing.T) {
	videos := SubjectID(KindNeverSynced, "videos")
	f := &DismissalFile{Version: 1, Dismissed: map[string]time.Time{
		videos: time.Now(),
	}}

	cases := []struct {
		name string
		id   string
		want bool
	}{
		{name: "empty id is never dismissed", id: "", want: false},
		{name: "unknown id", id: "something-else", want: false},
		{name: "dismissed id", id: videos, want: true},
		{name: "the v0.10.0 form of a dismissed id", id: "artifact-root-never-synced:videos", want: true},
		{
			// Per signature, not per kind: a different root is a different id.
			name: "same kind, different subject",
			id:   SubjectID(KindNeverSynced, "media"),
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

// camp v0.10.0 committed dismissals under ids that carried the whole root
// path. They must keep silencing their notices after the id change, and the
// next save must write them in the current form.
func TestDecodeDismissalsMigratesV010IDs(t *testing.T) {
	legacy := []byte(`version: 1
dismissed:
    artifact-root-never-synced:docs/marketing/content/visual-post/intent-backlog-clear-IB0001: 2026-09-01T10:00:00Z
    artifact-manifest-drift:media/renders: 2026-09-02T10:00:00Z
    artifact-roots-missing-locally: 2026-09-03T10:00:00Z
`)
	f, err := DecodeDismissals(legacy)
	if err != nil {
		t.Fatal(err)
	}

	never := SubjectID(KindNeverSynced, "docs/marketing/content/visual-post/intent-backlog-clear-IB0001")
	drift := SubjectID(KindManifestDrift, "media/renders")
	for _, id := range []string{never, drift, missingRootID} {
		if _, ok := f.Dismissed[id]; !ok {
			t.Errorf("decoded dismissals missing %q; have %v", id, f.Dismissed)
		}
	}
	if len(f.Dismissed) != 3 {
		t.Errorf("decoded %d dismissals, want 3: %v", len(f.Dismissed), f.Dismissed)
	}
	notices := []Notice{{ID: never}, {ID: drift}, {ID: missingRootID}, {ID: SubjectID(KindNeverSynced, "other")}}
	if kept := f.Filter(notices); len(kept) != 1 || kept[0].ID != SubjectID(KindNeverSynced, "other") {
		t.Errorf("Filter() kept %v, want only the undismissed root", kept)
	}

	out, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "artifact-root-never-synced:") || strings.Contains(string(out), "artifact-manifest-drift:") {
		t.Errorf("Encode() kept a v0.10.0 id:\n%s", out)
	}
	if !strings.Contains(string(out), never) || !strings.Contains(string(out), drift) {
		t.Errorf("Encode() is missing a migrated id:\n%s", out)
	}
}

// A file carrying both forms of one id (an old machine dismissed, then a new
// one did too) keeps the earlier dismissal.
func TestDecodeDismissalsKeepsEarliestOfBothForms(t *testing.T) {
	id := SubjectID(KindNeverSynced, "media")
	data := []byte("version: 1\ndismissed:\n" +
		"    " + id + ": 2026-09-05T00:00:00Z\n" +
		"    artifact-root-never-synced:media: 2026-09-01T00:00:00Z\n")
	f, err := DecodeDismissals(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Dismissed) != 1 {
		t.Fatalf("decoded %v, want one dismissal", f.Dismissed)
	}
	want := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if got := f.Dismissed[id]; !got.Equal(want) {
		t.Errorf("kept %v, want the earlier %v", got, want)
	}
}

func TestDismissalFileDismissAndRestoreAcceptV010IDs(t *testing.T) {
	f := &DismissalFile{Version: 1}
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	id := SubjectID(KindNeverSynced, "media")

	if !f.Dismiss("artifact-root-never-synced:media", at) {
		t.Fatal("Dismiss() of a v0.10.0 id = false, want true")
	}
	if _, ok := f.Dismissed[id]; !ok {
		t.Fatalf("Dismiss() stored %v, want the current id %q", f.Dismissed, id)
	}
	if f.Dismiss(id, at) {
		t.Error("Dismiss() of the current id after its v0.10.0 form = true, want false")
	}
	if !f.Restore("artifact-root-never-synced:media") {
		t.Fatal("Restore() of a v0.10.0 id = false, want true")
	}
	if f.IsDismissed(id) {
		t.Error("the id is still dismissed after Restore()")
	}
	if f.Restore(id) {
		t.Error("Restore() of an id that is not dismissed = true, want false")
	}
}

func TestDismissalFileDescribe(t *testing.T) {
	media := SubjectID(KindNeverSynced, "media")
	gone := SubjectID(KindManifestDrift, "removed")
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	f := &DismissalFile{Version: 1, Dismissed: map[string]time.Time{
		media: at, gone: at, missingRootID: at,
	}}

	got := f.Describe(map[string]string{media: "media"})
	if len(got) != 3 {
		t.Fatalf("Describe() returned %d entries, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].ID > got[i].ID {
			t.Errorf("Describe() is not in id order: %q before %q", got[i-1].ID, got[i].ID)
		}
	}
	byID := map[string]Dismissed{}
	for _, d := range got {
		byID[d.ID] = d
	}
	if d := byID[media]; d.Subject != "media" || d.Summary == "" || d.At != "2026-10-07" {
		t.Errorf("Describe()[media] = %+v", d)
	}
	if d := byID[gone]; d.Subject != "" || !HasSubject(d.ID) {
		t.Errorf("Describe()[undeclared root] = %+v, want no subject on a per-root id", d)
	}
	if d := byID[missingRootID]; d.Subject != "" || HasSubject(d.ID) || d.Summary == "" {
		t.Errorf("Describe()[missing roots] = %+v", d)
	}
}
