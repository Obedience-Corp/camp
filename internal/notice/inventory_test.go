package notice

import (
	"testing"
	"time"
)

func TestPartition_SplitsLiveFromDismissed(t *testing.T) {
	older := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	rawID := SubjectID(KindNeverSynced, "data/raw")
	dismissals := &DismissalFile{Dismissed: map[string]time.Time{
		DungeonLegacyID: older,
		rawID:           newer,
	}}
	detected := []Notice{
		{ID: DungeonLegacyID, Message: "legacy layout", Command: "camp dungeon migrate"},
		{ID: StaleLinksID, Message: "1 workitem link points at a path that no longer exists", Command: "camp workitem doctor --fix"},
	}

	inv := Partition(detected, dismissals, map[string]string{rawID: "data/raw"})

	if len(inv.Live) != 1 || inv.Live[0].ID != StaleLinksID {
		t.Fatalf("live = %+v, want only %s", inv.Live, StaleLinksID)
	}
	if len(inv.Dismissed) != 2 {
		t.Fatalf("dismissed = %+v, want 2 rows", inv.Dismissed)
	}
	first, second := inv.Dismissed[0], inv.Dismissed[1]
	if first.ID != rawID {
		t.Errorf("dismissed[0] = %s, want the newest dismissal first", first.ID)
	}
	if first.Notice != nil {
		t.Errorf("a dismissal no detector reports must carry no message, got %+v", first.Notice)
	}
	if first.Subject != "data/raw" || first.Summary != Summary(rawID) || first.Summary == "" {
		t.Errorf("an unreported dismissal should still say what it was about, got %+v", first)
	}
	if second.Notice == nil || second.Notice.Message != "legacy layout" {
		t.Errorf("a dismissal still detected must keep its message, got %+v", second.Notice)
	}
}

func TestPartition_DetectedSubjectWins(t *testing.T) {
	id := SubjectID(KindManifestDrift, "data/models")
	inv := Partition(
		[]Notice{{ID: id, Subject: "data/models", Message: "drifted", Command: "camp commit"}},
		&DismissalFile{Dismissed: map[string]time.Time{id: time.Now()}},
		map[string]string{},
	)
	if got := inv.Dismissed[0].Subject; got != "data/models" {
		t.Errorf("subject = %q, want the detector's subject when Subjects has none", got)
	}
}

func TestPartition_LegacyDismissalMatchesItsNotice(t *testing.T) {
	id := SubjectID(KindNeverSynced, "data/models")
	dismissals, err := DecodeDismissals([]byte("version: 1\ndismissed:\n  artifact-root-never-synced:data/models: 2026-10-01T00:00:00Z\n"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	inv := Partition([]Notice{{ID: id, Subject: "data/models", Message: "never synced"}}, dismissals, nil)
	if len(inv.Live) != 0 || len(inv.Dismissed) != 1 || inv.Dismissed[0].Notice == nil {
		t.Errorf("a v0.10.0 long-form dismissal should cover its notice, got %+v", inv)
	}
}

func TestPartition_EmptyIsArraysNotNil(t *testing.T) {
	inv := Partition(nil, &DismissalFile{}, nil)
	if inv.Live == nil || inv.Dismissed == nil {
		t.Fatalf("empty inventory must hold empty slices, got %+v", inv)
	}
	if !inv.Empty() {
		t.Error("Empty() = false for an inventory with nothing in it")
	}
}

func TestPartition_TiesOrderByID(t *testing.T) {
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	inv := Partition(nil, &DismissalFile{Dismissed: map[string]time.Time{"b": at, "a": at, "c": at}}, nil)
	got := []string{inv.Dismissed[0].ID, inv.Dismissed[1].ID, inv.Dismissed[2].ID}
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("order = %v, want [a b c]", got)
	}
}

func TestPartition_StripsDismissHintFromLiveCommand(t *testing.T) {
	id := "artifact-roots-missing-locally"
	detected := []Notice{{ID: id, Message: "1 declared artifact root is not on this machine",
		Command: "camp sync --from <machine>   (dismiss: camp notify dismiss " + id + ")"}}
	inv := Partition(detected, &DismissalFile{}, nil)
	if got := inv.Live[0].Command; got != "camp sync --from <machine>" {
		t.Errorf("command = %q, want the fix without the dismiss hint", got)
	}
}

func TestFixCommand(t *testing.T) {
	tests := []struct{ in, want string }{
		{"camp dungeon migrate", "camp dungeon migrate"},
		{"camp commit   (dismiss: camp notify dismiss manifest-drift-abc123)", "camp commit"},
		{"  camp workitem doctor --fix  ", "camp workitem doctor --fix"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := FixCommand(tt.in); got != tt.want {
			t.Errorf("FixCommand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRunnableCommand(t *testing.T) {
	tests := []struct {
		in        string
		want      string
		elsewhere bool
	}{
		{ElsewherePrefix + "camp sync --from <id of mac> --artifacts-only   (dismiss: camp notify dismiss x)",
			"camp sync --from <id of mac> --artifacts-only", true},
		{"camp dungeon migrate", "camp dungeon migrate", false},
	}
	for _, tt := range tests {
		got, elsewhere := RunnableCommand(tt.in)
		if got != tt.want || elsewhere != tt.elsewhere {
			t.Errorf("RunnableCommand(%q) = %q, %v; want %q, %v", tt.in, got, elsewhere, tt.want, tt.elsewhere)
		}
	}
}

func TestHasPlaceholder(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"camp sync --from <machine>", true},
		{"camp sync --from <id of mac-studio> --artifacts-only", true},
		{"camp sync --from <this-machine-id> --artifacts-only", true},
		{"camp dungeon migrate", false},
		{"camp workitem doctor --fix", false},
		{"echo a < b > c", false},
	}
	for _, tt := range tests {
		if got := HasPlaceholder(tt.in); got != tt.want {
			t.Errorf("HasPlaceholder(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
