package quest

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestChecklistItemStatus_ValidAndTerminal(t *testing.T) {
	cases := []struct {
		status   ChecklistItemStatus
		valid    bool
		terminal bool
	}{
		{ItemOpen, true, false},
		{ItemDoing, true, false},
		{ItemDone, true, true},
		{ItemDropped, true, true},
		{ChecklistItemStatus("bogus"), false, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.valid, tc.status.Valid(), "Valid(%q)", tc.status)
		assert.Equal(t, tc.terminal, tc.status.Terminal(), "Terminal(%q)", tc.status)
	}
}

func TestParseChecklistItemStatus(t *testing.T) {
	got, err := ParseChecklistItemStatus("  DOING ")
	require.NoError(t, err)
	assert.Equal(t, ItemDoing, got)

	_, err = ParseChecklistItemStatus("nope")
	require.ErrorIs(t, err, ErrInvalidChecklistStatus)
}

func TestGenerateChecklistItemID_FormatAndUniqueness(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	pattern := regexp.MustCompile(`^qci_20260710_[0-9a-f]{6}$`)

	seen := map[string]bool{}
	for range 200 {
		id, err := GenerateChecklistItemID(now, seen)
		require.NoError(t, err)
		assert.Regexp(t, pattern, id)
		require.False(t, seen[id], "GenerateChecklistItemID returned a duplicate: %s", id)
		seen[id] = true
	}
}

func TestGenerateChecklistItemID_SkipsCollision(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	// Reserve every id the generator could pick except by re-rolling; a small
	// pre-seeded set must never be returned.
	existing := map[string]bool{
		"qci_20260710_aaaaaa": true,
		"qci_20260710_bbbbbb": true,
	}
	for range 50 {
		id, err := GenerateChecklistItemID(now, existing)
		require.NoError(t, err)
		require.False(t, existing[id])
	}
}

func TestChecklist_Resolve(t *testing.T) {
	cl := &Checklist{
		QuestID: "qst_x",
		Items: []ChecklistItem{
			{ID: "qci_20260710_aa11bb", Title: "one"},
			{ID: "qci_20260710_cc22dd", Title: "two"},
			{ID: "qci_20260711_cc22ee", Title: "three"},
		},
	}

	// exact id
	got, err := cl.Resolve("qci_20260710_aa11bb")
	require.NoError(t, err)
	assert.Equal(t, "one", got.Title)

	// unique suffix
	got, err = cl.Resolve("aa11bb")
	require.NoError(t, err)
	assert.Equal(t, "one", got.Title)

	// ambiguous substring (cc22 matches two + three via their hex suffixes)
	_, err = cl.Resolve("cc22")
	require.ErrorIs(t, err, ErrChecklistItemAmbiguous)

	// the shared date segment must not fuzzy-match: substring resolution is
	// scoped to the hex suffix, so "20260710" resolves to nothing rather than
	// ambiguously matching every same-day item.
	_, err = cl.Resolve("20260710")
	require.ErrorIs(t, err, ErrChecklistItemNotFound)

	// not found
	_, err = cl.Resolve("zzz")
	require.ErrorIs(t, err, ErrChecklistItemNotFound)
}

func TestChecklist_NextRankAndSort(t *testing.T) {
	cl := &Checklist{}
	assert.Equal(t, rankStep, cl.NextRank())

	cl.Items = []ChecklistItem{
		{ID: "b", Rank: 30},
		{ID: "a", Rank: 10},
		{ID: "c", Rank: 20},
	}
	assert.Equal(t, 40, cl.NextRank())

	cl.Sort()
	assert.Equal(t, []string{"a", "c", "b"}, []string{cl.Items[0].ID, cl.Items[1].ID, cl.Items[2].ID})
}

func TestChecklist_SortTieBreaksByCreatedThenID(t *testing.T) {
	early := time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC)
	late := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	cl := &Checklist{Items: []ChecklistItem{
		{ID: "z", Rank: 10, CreatedAt: late},
		{ID: "y", Rank: 10, CreatedAt: early},
	}}
	cl.Sort()
	assert.Equal(t, "y", cl.Items[0].ID, "earlier CreatedAt sorts first at equal rank")
}

func TestChecklist_YAMLRoundTrip(t *testing.T) {
	completed := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	original := &Checklist{
		SchemaVersion: ChecklistSchemaV1,
		QuestID:       "qst_20260710_abc123",
		Items: []ChecklistItem{
			{
				ID:        "qci_20260710_aa11bb",
				Title:     "Ship recorder",
				Status:    ItemOpen,
				Rank:      10,
				Workitem:  &ChecklistWorkitem{ID: "feature-recorder-2026-07-10", Ref: "WI-abc123"},
				Notes:     "confirm scope",
				CreatedAt: completed,
				UpdatedAt: completed,
			},
			{
				ID:          "qci_20260710_cc22dd",
				Title:       "done thing",
				Status:      ItemDone,
				Rank:        20,
				CreatedAt:   completed,
				UpdatedAt:   completed,
				CompletedAt: &completed,
			},
		},
	}

	data, err := yaml.Marshal(original)
	require.NoError(t, err)

	var got Checklist
	require.NoError(t, yaml.Unmarshal(data, &got))

	require.Len(t, got.Items, 2)
	assert.Equal(t, ChecklistSchemaV1, got.SchemaVersion)
	assert.Equal(t, "qst_20260710_abc123", got.QuestID)
	assert.Equal(t, "feature-recorder-2026-07-10", got.Items[0].Workitem.ID)
	assert.Equal(t, ItemDone, got.Items[1].Status)
	require.NotNil(t, got.Items[1].CompletedAt)
	assert.True(t, got.Items[1].CompletedAt.Equal(completed))

	// A freeform item (no workitem) must round-trip with a nil workitem.
	assert.Nil(t, got.Items[1].Workitem)
}
