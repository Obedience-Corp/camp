package triage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFT013PRMergedAfterSnapshot is the sequence's Done When condition,
// replayed from the field trial: a PR merges minutes after the snapshot, and
// the verdict that rested on it must go stale with the anchor named.
func TestFT013PRMergedAfterSnapshot(t *testing.T) {
	ctx := context.Background()
	store, _, run := refreshStore(t)

	// As judged: the design was decided while its PR was still open.
	anchor := Anchor{
		Kind: AnchorKindPR, Repo: "Obedience-Corp/camp",
		Number: 546, Observed: "open",
	}
	record := validEvidence()
	record.StableID = hubID
	record.Anchors = []Anchor{anchor}
	_, err := store.WriteEvidence(ctx, run.ID, record)
	require.NoError(t, err)

	// Minutes later the PR merges.
	merged := &fakeRemote{byRepo: map[string]map[int]PRObservation{
		"Obedience-Corp/camp": {546: {State: "merged", SHA: "abc123"}},
	}}
	result, err := store.Refresh(ctx, RefreshInput{
		RunID: run.ID, Items: refreshItems(), Actor: "tester",
		Now: testAt.Add(3 * time.Minute), Remote: merged,
	})
	require.NoError(t, err)

	assert.Equal(t, ClassChanged, classOf(t, result, hubID),
		"a merged PR invalidates the verdict that rested on it (FT-013)")
	assert.Equal(t, []string{hubID}, result.StaleRecorded)
	assert.Equal(t, 1, result.RemoteResolved)
	assert.Equal(t, 0, result.RowsWithUncheckedAnchors(),
		"the anchor was checked, not skipped")

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, VerdictStale, verdicts[hubID].State)

	events, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	note := events[len(events)-1].Note
	assert.Contains(t, note, "pr:Obedience-Corp/camp#546",
		"the reason must name the anchor that expired")
	assert.Contains(t, note, "open")
	assert.Contains(t, note, "merged")

	// The verdict is now unapplicable, which is what apply will refuse on.
	for _, row := range result.Diff.Rows {
		if row.StableID == hubID {
			assert.Equal(t, ApplyBlockedStale,
				ApplyReadinessFor(row, CanonicalAction("dungeon/completed"), false))
		}
	}

}

// TestOfflineBlocksTerminalActionsOnly is spec doc 04's offline rule: an
// unverifiable anchor is fresh enough to park a workitem and not fresh enough
// to retire one.
func TestOfflineBlocksTerminalActionsOnly(t *testing.T) {
	ctx := context.Background()
	store, _, run := refreshStore(t)

	record := validEvidence()
	record.StableID = hubID
	record.Anchors = []Anchor{{
		Kind: AnchorKindPR, Repo: "Obedience-Corp/camp", Number: 546, Observed: "open",
	}}
	_, err := store.WriteEvidence(ctx, run.ID, record)
	require.NoError(t, err)

	// No remote checker at all: the offline case.
	result, err := store.Refresh(ctx, RefreshInput{
		RunID: run.ID, Items: refreshItems(), Actor: "tester", Now: testAt,
	})
	require.NoError(t, err)

	assert.Equal(t, ClassFresh, classOf(t, result, hubID),
		"offline must not invalidate a run")
	assert.Equal(t, 1, result.RowsWithUncheckedAnchors())
	assert.Empty(t, result.StaleRecorded)

	var row RowDiff
	for _, r := range result.Diff.Rows {
		if r.StableID == hubID {
			row = r
		}
	}
	require.Equal(t, 1, row.UncheckedAnchors)

	tests := []struct {
		name   string
		action CanonicalAction
		force  bool
		want   ApplyReadiness
	}{
		{
			name:   "a non-terminal action applies over an unchecked anchor",
			action: CanonicalAction("attention/parked"),
			want:   ApplyReady,
		},
		{
			name:   "a terminal dungeon action is blocked",
			action: CanonicalAction("dungeon/completed"),
			want:   ApplyBlockedUnchecked,
		},
		{
			name:   "a split is terminal and blocked",
			action: ActionSplit,
			want:   ApplyBlockedUnchecked,
		},
		{
			name:   "--force overrides the unchecked block",
			action: CanonicalAction("dungeon/completed"),
			force:  true,
			want:   ApplyReady,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyReadinessFor(row, tt.action, tt.force)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.want != ApplyReady, got.Blocked())
		})
	}
}

// TestApplyReadinessStaleIsNotForceable draws the line the offline rule does
// not cross: --force covers missing information, never contradicted
// information.
func TestApplyReadinessStaleIsNotForceable(t *testing.T) {
	for _, class := range []RowClass{ClassChanged, ClassGone, ClassNew} {
		row := RowDiff{Class: class, UncheckedAnchors: 0}
		assert.Equal(t, ApplyBlockedStale,
			ApplyReadinessFor(row, CanonicalAction("dungeon/completed"), true),
			"class %q must stay blocked even with --force", class)
	}
}

// TestRefreshRecordsALostCarryWithItsReason covers the profile-delta path the
// class alone cannot see: nothing in the world moved, but the policy that
// produced the verdict did.
func TestRefreshRecordsALostCarryWithItsReason(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := NewStore(root, func() time.Time { return testAt })

	manifest := newManifestForStore()
	manifest.Rows[0].CarriedFrom = ptr("run-20260803T234110Z")
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	for _, row := range run.Manifest.Rows {
		record := validEvidence()
		record.StableID = row.StableID
		record.Anchors = nil
		_, err := store.WriteEvidence(ctx, run.ID, record)
		require.NoError(t, err)
		_, err = store.Propose(ctx, ProposeInput{
			RunID: run.ID, StableID: row.StableID, Disposition: "parked",
			Rationale: rationale("because"), Actor: "tester", Now: testAt,
		})
		require.NoError(t, err)
	}

	// The row's own lane changes evidence depth: a touching delta.
	next := DefaultProfile()
	next.Normalize()
	next.Evidence.DepthByStage[run.Manifest.Rows[0].AttentionStage] = EvidenceDepthNone

	result, err := store.Refresh(ctx, RefreshInput{
		RunID: run.ID, Items: refreshItems(), Actor: "tester", Now: testAt,
		CurrentProfile: &next,
	})
	require.NoError(t, err)

	assert.Equal(t, ClassFresh, classOf(t, result, hubID),
		"the world did not move; only the policy did")
	require.Len(t, result.CarryLost, 1)
	assert.Equal(t, hubID, result.CarryLost[0].StableID)
	assert.Contains(t, result.CarryLost[0].Reason, "evidence depth for stage")
	assert.Equal(t, []string{hubID}, result.StaleRecorded)

	// And status can answer why, without any extra record.
	status, err := BuildStatus(ctx, store, run.ID)
	require.NoError(t, err)
	require.Len(t, status.CarryLosses, 1)
	assert.Equal(t, hubID, status.CarryLosses[0].StableID)
	assert.Contains(t, status.CarryLosses[0].Reason, "carry lost")
	assert.Contains(t, status.CarryLosses[0].Reason, "evidence depth")
}

// TestRefreshDoesNotDoubleRecordACarriedChangedRow: a carried row that also
// changed must retire once, not twice.
func TestRefreshDoesNotDoubleRecordACarriedChangedRow(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := NewStore(root, func() time.Time { return testAt })

	manifest := newManifestForStore()
	manifest.Rows[0].CarriedFrom = ptr("run-20260803T234110Z")
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	record := validEvidence()
	record.StableID = hubID
	record.Anchors = nil
	_, err = store.WriteEvidence(ctx, run.ID, record)
	require.NoError(t, err)
	_, err = store.Propose(ctx, ProposeInput{
		RunID: run.ID, StableID: hubID, Disposition: "parked",
		Rationale: rationale("because"), Actor: "tester", Now: testAt,
	})
	require.NoError(t, err)

	before, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)

	// The row is gone, and it was carried: both paths want to retire it.
	result, err := store.Refresh(ctx, RefreshInput{
		RunID: run.ID, Items: nil, Actor: "tester", Now: testAt,
	})
	require.NoError(t, err)

	after, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	assert.Len(t, after, len(before)+1,
		"one invalidation appends one retirement, not two")
	assert.Equal(t, []string{hubID}, result.StaleRecorded)
	require.Len(t, result.CarryLost, 1, "the loss is still reported")
}
