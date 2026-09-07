package state

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathMatches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		visit, item string
		want        bool
	}{
		{"projects/camp", "projects/camp", true},
		{"projects/camp/cmd", "projects/camp", true},
		{"projects/camp-timeline", "projects/camp", false},
		{"projects/camp", "projects/camp-timeline", false},
		{"projects/camp-timeline", "projects/camp-timeline", true},
		{"projects/camp-timeline", "projects", true},
		{"workflow/design/x", "workflow", true},
		{"", "projects", false},
		{"projects", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.visit+"→"+tt.item, func(t *testing.T) {
			t.Parallel()
			if got := pathMatches(tt.visit, tt.item); got != tt.want {
				t.Fatalf("pathMatches(%q, %q) = %v, want %v", tt.visit, tt.item, got, tt.want)
			}
		})
	}
}

func TestRankTimeUnranked(t *testing.T) {
	t.Parallel()
	ranks := RankMap(nil, t.TempDir())
	if !RankTime(ranks, "projects/camp").IsZero() {
		t.Fatal("unranked item must have zero RankTime")
	}
}

func TestAppendUniqueDedupAndVisitDrop(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	a := NavigationEntry{Rel: "projects/camp", Kind: KindToggle, Time: now, Location: "/x/camp"}
	b := NavigationEntry{Rel: "projects/camp", Kind: KindToggle, Time: now.Add(time.Second), Location: "/x/camp"}
	got := appendUnique([]NavigationEntry{a}, b)
	require.Len(t, got, 1)
	assert.Equal(t, b.Time, got[0].Time)

	visit := NavigationEntry{Rel: "projects/camp", Kind: KindVisit, Time: now, Location: "/x/camp"}
	got = appendUnique(got, visit)
	require.Len(t, got, 2, "toggle and visit for the same rel both stay")

	dropped := appendUnique(got, NavigationEntry{Rel: "", Kind: KindVisit, Location: "/x"})
	require.Len(t, dropped, 2, "empty-rel visit is dropped")
}

func TestRecordJumpDestStrictlyAfterSource(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	fest := filepath.Join(root, "projects", "fest")
	camp := filepath.Join(root, "projects", "camp")
	require.NoError(t, os.MkdirAll(fest, 0755))
	require.NoError(t, os.MkdirAll(camp, 0755))

	frozen := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	require.NoError(t, recordJumpAt(ctx, root, fest, camp, frozen))

	entries, err := LoadHistory(ctx, root)
	require.NoError(t, err)
	ranks := RankMap(entries, root)
	if !RankTime(ranks, "projects/camp").After(RankTime(ranks, "projects/fest")) {
		t.Fatalf("dest rank %v is not after source rank %v", RankTime(ranks, "projects/camp"), RankTime(ranks, "projects/fest"))
	}
	last, err := GetLastLocation(ctx, root)
	require.NoError(t, err)
	assert.Equal(t, fest, last, "GetLastLocation must return toggle source, not dest visit")
}

func TestRecordJumpSkipsVisitWhenDestIsRoot(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	proj := filepath.Join(root, "projects", "camp")
	require.NoError(t, os.MkdirAll(proj, 0755))

	require.NoError(t, RecordJump(ctx, root, proj, root))
	entries, err := LoadHistory(ctx, root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, KindToggle, entries[0].Kind)
	assert.Equal(t, "projects/camp", entries[0].Rel)
}

func TestRecordJumpSkipsVisitWhenDestEqualsSrc(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	proj := filepath.Join(root, "projects", "camp")
	require.NoError(t, os.MkdirAll(proj, 0755))

	require.NoError(t, RecordJump(ctx, root, proj, proj))
	entries, err := LoadHistory(ctx, root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, KindToggle, entries[0].Kind)
}

func TestRecordJumpPersistsRootToggle(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	proj := filepath.Join(root, "projects", "camp")
	require.NoError(t, os.MkdirAll(proj, 0755))

	require.NoError(t, RecordJump(ctx, root, root, proj))
	entries, err := LoadHistory(ctx, root)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, KindToggle, entries[0].Kind)
	assert.Equal(t, "", entries[0].Rel)
	assert.Equal(t, KindVisit, entries[1].Kind)
	assert.Equal(t, "projects/camp", entries[1].Rel)

	last, err := GetLastLocation(ctx, root)
	require.NoError(t, err)
	assert.Equal(t, root, last)
}

func TestOldLogEmptyKindIsToggleAndRanks(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	fest := filepath.Join(root, "projects", "fest")
	camp := filepath.Join(root, "projects", "camp")
	require.NoError(t, os.MkdirAll(fest, 0755))
	require.NoError(t, os.MkdirAll(camp, 0755))

	stateFile := StatePath(root)
	require.NoError(t, os.MkdirAll(filepath.Dir(stateFile), 0755))
	old := NavigationEntry{
		Location: fest,
		Time:     time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
	}
	raw, err := json.Marshal(old)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(stateFile, append(raw, '\n'), 0600))

	entries, err := LoadHistory(ctx, root)
	require.NoError(t, err)
	last, err := GetLastLocation(ctx, root)
	require.NoError(t, err)
	assert.Equal(t, fest, last, "empty kind counts as toggle")

	ranks := RankMap(entries, root)
	if RankTime(ranks, "projects/fest").IsZero() {
		t.Fatal("empty-kind line must still rank as a visit")
	}

	require.NoError(t, RecordJump(ctx, root, fest, camp))
	entries, err = LoadHistory(ctx, root)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEmpty(t, e.Kind, "rewrite must backfill kind")
	}
}

func TestUniqueLRUDropsFront(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	first := filepath.Join(root, "p", "first")
	require.NoError(t, os.MkdirAll(first, 0755))
	require.NoError(t, SetLastLocation(ctx, root, first))

	for i := range 256 {
		p := filepath.Join(root, "p", "n", strconv.Itoa(i))
		require.NoError(t, os.MkdirAll(p, 0755))
		require.NoError(t, SetLastLocation(ctx, root, p))
	}
	entries, err := LoadHistory(ctx, root)
	require.NoError(t, err)
	require.Len(t, entries, 256)
	for _, e := range entries {
		if e.Location == first {
			t.Fatal("first unique rel must be dropped when the 257th is written")
		}
	}
}

func TestDuplicateRelKindCollapses(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	p := filepath.Join(root, "projects", "camp")
	require.NoError(t, os.MkdirAll(p, 0755))
	require.NoError(t, SetLastLocation(ctx, root, p))
	require.NoError(t, SetLastLocation(ctx, root, p))
	entries, err := LoadHistory(ctx, root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestRecordJumpContextCancel(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "projects", "camp")
	require.NoError(t, os.MkdirAll(p, 0755))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.Error(t, RecordJump(ctx, root, p, p))
	require.Error(t, RecordVisit(ctx, root, p))
}

func TestRecordVisitDroppedForRoot(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	require.NoError(t, RecordVisit(ctx, root, root))
	entries, err := LoadHistory(ctx, root)
	require.NoError(t, err)
	assert.Empty(t, entries)
}
