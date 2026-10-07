package notice

import (
	"regexp"
	"strings"
	"testing"
)

var subjectIDPattern = regexp.MustCompile(`^(never-synced|manifest-drift)-[0-9a-f]{6}$`)

func TestSubjectIDIsShortAndStable(t *testing.T) {
	long := "docs/marketing/content/visual-post/intent-backlog-clear-IB0001"

	id := SubjectID(KindNeverSynced, long)
	if !subjectIDPattern.MatchString(id) {
		t.Fatalf("SubjectID() = %q, want <kind>-<6 hex>", id)
	}
	if strings.Contains(id, "docs/") {
		t.Errorf("SubjectID() = %q leaks the subject into the id", id)
	}
	if again := SubjectID(KindNeverSynced, long); again != id {
		t.Errorf("SubjectID() is not stable: %q then %q", id, again)
	}
	if other := SubjectID(KindNeverSynced, "media/renders"); other == id {
		t.Errorf("two subjects share id %q", id)
	}
	if drift := SubjectID(KindManifestDrift, long); drift == id {
		t.Errorf("two kinds share id %q", id)
	}
}

func TestCanonicalIDMapsV010LongForm(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want string
	}{
		{
			name: "never synced",
			id:   "artifact-root-never-synced:docs/marketing/content/visual-post/intent-backlog-clear-IB0001",
			want: SubjectID(KindNeverSynced, "docs/marketing/content/visual-post/intent-backlog-clear-IB0001"),
		},
		{
			name: "manifest drift",
			id:   "artifact-manifest-drift:media/renders",
			want: SubjectID(KindManifestDrift, "media/renders"),
		},
		{
			name: "hand-typed trailing slash names the same root",
			id:   "artifact-root-never-synced:media/renders/",
			want: SubjectID(KindNeverSynced, "media/renders"),
		},
		{name: "current id passes through", id: SubjectID(KindNeverSynced, "x"), want: SubjectID(KindNeverSynced, "x")},
		{name: "subject-less id passes through", id: missingRootID, want: missingRootID},
		{name: "legacy prefix with no subject is left alone", id: "artifact-root-never-synced:", want: "artifact-root-never-synced:"},
		{name: "unknown id passes through", id: "something-else", want: "something-else"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanonicalID(tc.id); got != tc.want {
				t.Errorf("CanonicalID(%q) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}

// Subject-less ids are already committed in users' dismissal files under these
// exact strings, so they must never change.
func TestSubjectlessIDsAreUnchanged(t *testing.T) {
	for id, want := range map[string]string{
		missingRootID:   "artifact-roots-missing-locally",
		DungeonLegacyID: "dungeon-legacy-layout",
		StaleLinksID:    "workitem-links-stale",
	} {
		if id != want {
			t.Errorf("subject-less id changed: %q, want %q", id, want)
		}
	}
}

func TestSummaryCoversEveryNotice(t *testing.T) {
	for _, id := range []string{
		SubjectID(KindNeverSynced, "a"),
		SubjectID(KindManifestDrift, "a"),
		missingRootID,
		DungeonLegacyID,
		StaleLinksID,
	} {
		if Summary(id) == "" {
			t.Errorf("Summary(%q) is empty", id)
		}
	}
	if got := Summary("not-a-notice"); got != "" {
		t.Errorf("Summary(unknown) = %q, want empty", got)
	}
}
