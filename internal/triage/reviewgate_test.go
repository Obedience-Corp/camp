package triage

import (
	"context"
	"strconv"
	"sync"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- the judging -> reviewing gate --------------------------------------

// TestReviewGateBlocksWithNamedGaps: rows nobody looked at would reach a
// reviewer as blanks and be approved by omission.
func TestReviewGateBlocksWithNamedGaps(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)
	_, err = store.SetPhase(ctx, run.ID, PhaseSnapshotted, "")
	require.NoError(t, err)
	_, err = store.SetPhase(ctx, run.ID, PhaseJudging, "")
	require.NoError(t, err)

	_, err = store.SetPhase(ctx, run.ID, PhaseReviewing, "")

	require.Error(t, err)
	require.ErrorIs(t, err, camperrors.ErrInvalidInput)
	for _, row := range run.Manifest.Rows {
		assert.Contains(t, err.Error(), row.StableID, "every gap is named")
	}
	assert.Contains(t, err.Error(), "needs evidence")
	assert.Contains(t, err.Error(), "--no-evidence", "and the way to close it")

	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, PhaseJudging, reopened.State.Phase, "a refused transition changes nothing")
}

// TestReviewGateNamesTheMissingHalf distinguishes "nobody read it" from
// "nobody concluded anything", because the fix differs.
func TestReviewGateNamesTheMissingHalf(t *testing.T) {
	ctx := context.Background()
	store, run := proposeStore(t)
	gaps, err := store.ReadyForReview(ctx, run.ID)

	require.NoError(t, err)
	require.Len(t, gaps, len(run.Manifest.Rows))
	for _, gap := range gaps {
		assert.Equal(t, "proposal", gap.Missing,
			"evidence exists for every row, so the missing half is the proposal")
	}
}

// TestReviewGateOpensOnceEveryRowIsJudged.
func TestReviewGateOpensOnceEveryRowIsJudged(t *testing.T) {
	ctx := context.Background()
	store, run := proposeStore(t)
	for _, row := range run.Manifest.Rows {
		_, err := store.Propose(ctx, ProposeInput{
			RunID: run.ID, StableID: row.StableID, Disposition: "parked",
			Rationale: rationale("later"), Actor: "tester", Now: testAt,
		})
		require.NoError(t, err)
	}

	gaps, err := store.ReadyForReview(ctx, run.ID)
	require.NoError(t, err)
	assert.Empty(t, gaps)

	_, err = store.SetPhase(ctx, run.ID, PhaseSnapshotted, "")
	require.NoError(t, err)
	_, err = store.SetPhase(ctx, run.ID, PhaseJudging, "")
	require.NoError(t, err)
	moved, err := store.SetPhase(ctx, run.ID, PhaseReviewing, "")

	require.NoError(t, err)
	assert.Equal(t, PhaseReviewing, moved.State.Phase)
}

// TestNoEvidenceMarkerSatisfiesTheGate: deciding without a gathered record is
// a real answer, so it must not hold the run in judging forever.
func TestNoEvidenceMarkerSatisfiesTheGate(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	for _, row := range run.Manifest.Rows {
		marker := &EvidenceRecord{
			SchemaVersion: SchemaVersion,
			StableID:      row.StableID,
			NoEvidence:    true,
			ProducedBy:    ProducedBy{Role: EvidenceRoleHuman, Runtime: "human", At: testAt},
		}
		_, err := store.WriteEvidence(ctx, run.ID, marker)
		require.NoError(t, err)
		_, err = store.Propose(ctx, ProposeInput{
			RunID: run.ID, StableID: row.StableID, Disposition: "parked",
			Rationale: rationale("judged from the card"), Actor: "tester", Now: testAt,
		})
		require.NoError(t, err)
	}

	gaps, err := store.ReadyForReview(ctx, run.ID)

	require.NoError(t, err)
	assert.Empty(t, gaps)
}

// TestCarriedRowsDoNotBlockTheGate: they were decided in a previous run on
// purpose.
func TestCarriedRowsDoNotBlockTheGate(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)
	carried := run.Manifest.Rows[1]
	require.NotNil(t, carried.CarriedFrom)

	gaps, err := store.ReadyForReview(ctx, run.ID)

	require.NoError(t, err)
	for _, gap := range gaps {
		assert.NotEqual(t, carried.StableID, gap.StableID)
	}
}

// TestQueueAndGateAgreeOnWhatCountsAsJudged is the finding this test exists
// for. The two derived "is this row judged" separately and disagreed: the gate
// treated a staled or rejected verdict as satisfied while the queue still
// listed the row as needing work, so a run refresh had just invalidated could
// pass into review with rows nobody had re-judged.
func TestQueueAndGateAgreeOnWhatCountsAsJudged(t *testing.T) {
	tests := []struct {
		name    string
		kind    DecisionEventKind
		judged  bool
		missing string
	}{
		{name: "a live proposal is judged", kind: DecisionProposed, judged: true},
		{name: "an approval is judged", kind: DecisionApproved, judged: true},
		{name: "a rejection needs a new proposal", kind: DecisionRejected, missing: "proposal"},
		{name: "a staled verdict needs a new proposal", kind: DecisionStale, missing: "proposal"},
		{name: "a superseded proposal needs a new one", kind: DecisionSuperseded, missing: "proposal"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, run := proposeStore(t)
			target := run.Manifest.Rows[0].StableID

			decision := event(tc.kind, "parked", "attention/parked")
			decision.StableID = target
			require.NoError(t, store.AppendDecision(ctx, run.ID, decision))

			queue, err := BuildQueue(ctx, store, run.ID, "")
			require.NoError(t, err)
			queued := false
			for _, item := range queue.Items {
				if item.StableID == target {
					queued = true
				}
			}

			gaps, err := store.ReadyForReview(ctx, run.ID)
			require.NoError(t, err)
			var gap *ReviewGap
			for i := range gaps {
				if gaps[i].StableID == target {
					gap = &gaps[i]
				}
			}

			if tc.judged {
				assert.False(t, queued, "the queue considers it done")
				assert.Nil(t, gap, "and so must the gate")
				return
			}
			assert.True(t, queued, "the queue considers it unfinished")
			require.NotNil(t, gap, "and so must the gate")
			assert.Equal(t, tc.missing, gap.Missing)
		})
	}
}

// TestConcurrentProposesOnOneRowLeaveOneLiveProposal asserts the verdict
// stream stays well formed under concurrent proposals on one row.
//
// Honest scope: this is a well-formedness guard, not proof that the per-row
// propose lock is load-bearing. Removing that lock does not make this test
// fail, because AppendDecision already serializes on the stream's own lock and
// the remaining read-modify-write window is too narrow to hit here. The lock
// stays because a three-step read-modify-write without one is a real bug class
// — the same one that was reproducible and mutation-verified for CreateRun in
// the first sequence — and the window widens with drivers and load. What this
// test does catch is an ordering regression: a proposal recorded while another
// is still live, or a retirement of nothing.
func TestConcurrentProposesOnOneRowLeaveOneLiveProposal(t *testing.T) {
	ctx := context.Background()
	store, run := proposeStore(t)
	target := run.Manifest.Rows[0].StableID
	_, err := store.Propose(ctx, ProposeInput{
		RunID: run.ID, StableID: target, Disposition: "completed",
		Rationale: rationale("first"), Actor: "tester", Now: testAt,
	})
	require.NoError(t, err)

	const racers = 4
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = store.Propose(ctx, ProposeInput{
				RunID: run.ID, StableID: target, Disposition: "parked",
				Rationale: rationale("racer " + strconv.Itoa(i)), Actor: "tester", Now: testAt,
			})
		}()
	}
	close(start)
	wg.Wait()

	events, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)

	// The invariant is ordering, not counts: a proposal may only be recorded
	// when none is live, so every `proposed` after the first must follow a
	// `superseded`. Counting events alone would be satisfied by the broken
	// interleaving too, since each racer emits one of each.
	live := false
	for i, e := range events {
		if e.StableID != target {
			continue
		}
		switch e.Event {
		case DecisionProposed:
			assert.False(t, live,
				"event %d recorded a proposal while one was already live", i)
			live = true
		case DecisionSuperseded:
			assert.True(t, live,
				"event %d retired a proposal when none was live", i)
			live = false
		}
	}
	assert.True(t, live, "the stream must end with exactly one live proposal")

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, VerdictProposed, verdicts[target].State, "exactly one proposal is live")
}

// TestSupersededRowReopensTheGate: a row whose only proposal was retired is
// back to needing one.
func TestSupersededRowReopensTheGate(t *testing.T) {
	ctx := context.Background()
	store, run := proposeStore(t)
	target := run.Manifest.Rows[0].StableID
	_, err := store.Propose(ctx, ProposeInput{
		RunID: run.ID, StableID: target, Disposition: "parked",
		Rationale: rationale("first"), Actor: "tester", Now: testAt,
	})
	require.NoError(t, err)

	superseded := event(DecisionSuperseded, "", "")
	superseded.StableID = target
	require.NoError(t, store.AppendDecision(ctx, run.ID, superseded))

	gaps, err := store.ReadyForReview(ctx, run.ID)

	require.NoError(t, err)
	var found bool
	for _, gap := range gaps {
		if gap.StableID == target {
			found = true
			assert.Equal(t, "proposal", gap.Missing)
		}
	}
	assert.True(t, found, "a retired proposal leaves the row needing a new one")
}
