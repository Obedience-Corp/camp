package triage

import (
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selectorFixture builds a run with a mixed lane: two non-terminal rows and
// two terminal ones sharing a batch, which is the shape the exclusion rule
// exists for.
func selectorFixture() (*Run, map[string]RowVerdict) {
	manifest := &Manifest{
		SchemaVersion: SchemaVersion,
		RunID:         "run-20260810T140000Z",
		Mode:          RunModeFull,
		Profile:       ManifestProfile{Name: ProfileNameDefault, Resolved: DefaultProfile()},
		CreatedAt:     testAt,
	}
	add := func(id string, batch int) {
		manifest.Rows = append(manifest.Rows, ManifestRow{
			StableID: id, Key: "design:" + id, Type: "design", Title: id,
			RelativePath: "workflow/design/" + id, LifecycleStage: "active",
			Batch:  batch,
			Policy: RowPolicy{Evidence: EvidenceDepthDeep, RoutingTier: RoutingTierDefault},
		})
	}
	add("park-a", 1)
	add("park-b", 1)
	add("retire-a", 1)
	add("split-a", 1)
	add("unjudged", 2)
	add("park-c", 2)
	manifest.Normalize()

	run := &Run{
		ID: manifest.RunID, Dir: "/campaign/run", Manifest: manifest,
		State: &RunState{
			SchemaVersion: SchemaVersion, RunID: manifest.RunID, Phase: PhaseReviewing,
			PhaseHistory: []PhaseTransition{{Phase: PhaseCreated, At: testAt}},
		},
	}

	proposed := func(disposition string, action CanonicalAction) RowVerdict {
		return RowVerdict{State: VerdictProposed, Disposition: disposition,
			CanonicalAction: action, Actor: "tester", At: testAt}
	}
	verdicts := map[string]RowVerdict{
		"park-a":   proposed("parked", "attention/parked"),
		"park-b":   proposed("parked", "attention/parked"),
		"park-c":   proposed("parked", "attention/parked"),
		"retire-a": proposed("completed", "dungeon/completed"),
		"split-a":  proposed("consolidate", ActionSplit),
	}
	return run, verdicts
}

// --- error cases first -------------------------------------------------

// TestExpandSelectorRejectsUnknownRows names the id rather than silently
// approving the subset it recognized.
func TestExpandSelectorRejectsUnknownRows(t *testing.T) {
	run, verdicts := selectorFixture()

	_, err := ExpandSelector(run, verdicts, Selector{StableIDs: []string{"park-a", "ghost"}})

	require.Error(t, err)
	require.ErrorIs(t, err, camperrors.ErrInvalidInput)
	assert.Contains(t, err.Error(), "ghost")
}

// TestExpandSelectorRejectsUnknownLaneAndBatch lists what would have worked.
func TestExpandSelectorRejectsUnknownLaneAndBatch(t *testing.T) {
	run, verdicts := selectorFixture()

	_, err := ExpandSelector(run, verdicts, Selector{Lane: "nonexistent"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "park-for-later", "the refusal lists real lanes")

	_, err = ExpandSelector(run, verdicts, Selector{Batch: 99})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "batch")
}

// TestExpandSelectorRequiresAForm refuses an empty selector rather than
// defaulting to everything.
func TestExpandSelectorRequiresAForm(t *testing.T) {
	run, verdicts := selectorFixture()

	_, err := ExpandSelector(run, verdicts, Selector{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--lane")
}

// --- the terminal exclusion --------------------------------------------

// TestBulkSelectorSkipsTerminalRows is the safety rule this sequence exists
// for. Approving a batch is not meaningful consent to each irreversible action
// inside it, so terminal rows are listed and left for an explicit approval.
func TestBulkSelectorSkipsTerminalRows(t *testing.T) {
	run, verdicts := selectorFixture()

	tests := []struct {
		name     string
		selector Selector
	}{
		{"by batch", Selector{Batch: 1}},
		{"by lane", Selector{Lane: "park-for-later"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			selection, err := ExpandSelector(run, verdicts, tc.selector)
			require.NoError(t, err)
			selection.Sort()

			for _, row := range selection.Matched {
				assert.False(t, verdicts[row.StableID].CanonicalAction.Terminal(),
					"a bulk selector never matches a terminal row: %s", row.StableID)
			}
		})
	}
}

// TestBatchSelectorListsTheTerminalRowsItSkipped: skipping silently would be
// worse than not skipping, because the operator would think the batch was done.
func TestBatchSelectorListsTheTerminalRowsItSkipped(t *testing.T) {
	run, verdicts := selectorFixture()

	selection, err := ExpandSelector(run, verdicts, Selector{Batch: 1})
	require.NoError(t, err)
	selection.Sort()

	assert.Equal(t, []string{"park-a", "park-b"}, idsOf(selection.Matched))
	assert.Equal(t, []string{"retire-a", "split-a"}, selection.TerminalIDs(),
		"both a dungeon move and a split are terminal")
}

// TestNamingATerminalRowIsConsent: the exclusion is about bulk selection, not
// about forbidding the action.
func TestNamingATerminalRowIsConsent(t *testing.T) {
	run, verdicts := selectorFixture()

	selection, err := ExpandSelector(run, verdicts, Selector{StableIDs: []string{"retire-a", "split-a"}})

	require.NoError(t, err)
	assert.Equal(t, []string{"retire-a", "split-a"}, idsOf(selection.Matched))
	assert.Empty(t, selection.SkippedTerminal)
}

// --- rows with nothing to approve --------------------------------------

// TestSelectorSkipsRowsWithNoProposal covers both selector forms.
func TestSelectorSkipsRowsWithNoProposal(t *testing.T) {
	run, verdicts := selectorFixture()

	byBatch, err := ExpandSelector(run, verdicts, Selector{Batch: 2})
	require.NoError(t, err)
	byBatch.Sort()
	assert.Equal(t, []string{"park-c"}, idsOf(byBatch.Matched))
	assert.Equal(t, []string{"unjudged"}, idsOf(byBatch.SkippedNoProposal))

	byID, err := ExpandSelector(run, verdicts, Selector{StableIDs: []string{"unjudged"}})
	require.NoError(t, err)
	assert.Empty(t, byID.Matched, "naming a row does not invent a proposal for it")
	assert.Equal(t, []string{"unjudged"}, idsOf(byID.SkippedNoProposal))
}

// TestEmptyReportsNothingAtAll distinguishes "matched nothing" from "matched
// rows it then skipped", which the caller turns into different outcomes.
func TestEmptyReportsNothingAtAll(t *testing.T) {
	run, verdicts := selectorFixture()

	skippedOnly, err := ExpandSelector(run, verdicts, Selector{StableIDs: []string{"unjudged"}})
	require.NoError(t, err)
	assert.False(t, skippedOnly.Empty(), "a skipped row is still a match to report")

	assert.True(t, (&Selection{}).Empty())
}

// --- ordering ----------------------------------------------------------

// TestSelectionSortsByStableID keeps recorded events and printed output in the
// same order run to run, regardless of manifest order.
func TestSelectionSortsByStableID(t *testing.T) {
	run, verdicts := selectorFixture()

	selection, err := ExpandSelector(run, verdicts, Selector{
		StableIDs: []string{"park-b", "park-a"},
	})
	require.NoError(t, err)
	selection.Sort()

	assert.Equal(t, []string{"park-a", "park-b"}, idsOf(selection.Matched))
}

// TestSelectorDescribeReadsWell: the description appears in refusals, so it
// has to name what the operator typed.
func TestSelectorDescribeReadsWell(t *testing.T) {
	assert.Equal(t, `lane "parked"`, Selector{Lane: "parked"}.Describe())
	assert.Equal(t, "batch 3", Selector{Batch: 3}.Describe())
	assert.Equal(t, `"row-1"`, Selector{StableIDs: []string{"row-1"}}.Describe())
	assert.Equal(t, "2 rows", Selector{StableIDs: []string{"a", "b"}}.Describe())
}

// TestLaneSelectorNamesAreTypeable: a lane heading is prose, and the selector
// token has to be something a user can actually type.
func TestLaneSelectorNamesAreTypeable(t *testing.T) {
	run, verdicts := selectorFixture()

	names := LaneNames(run, verdicts)

	assert.Contains(t, names, "park-for-later")
	assert.Contains(t, names, "close-as-delivered")
	for _, name := range names {
		assert.NotContains(t, name, " ", "a selector token has no spaces: %q", name)
	}
}
