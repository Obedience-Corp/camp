package triage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusRun builds an opened run from the shared manifest fixture.
func statusRun(t *testing.T) *Run {
	t.Helper()
	manifest := validManifest()
	manifest.Rows[1].CarriedFrom = nil
	manifest.Rows[1].IdentityException = nil
	return &Run{
		ID:       manifest.RunID,
		Dir:      "/campaign/.campaign/triage/runs/" + manifest.RunID,
		Manifest: manifest,
		State:    validRunState(),
	}
}

// --- shape -------------------------------------------------------------

// TestStatusCountsEveryStateEvenAtZero keeps the JSON shape fixed, so a
// consumer can index counts without checking for presence.
func TestStatusCountsEveryStateEvenAtZero(t *testing.T) {
	status := StatusFrom(statusRun(t), nil)

	require.Len(t, status.Counts, len(RowStates()))
	for _, state := range RowStates() {
		_, ok := status.Counts[state]
		assert.True(t, ok, "counts must include %s", state)
	}
	assert.NotNil(t, status.Batches)
	assert.NotNil(t, status.Consolidations,
		"the consolidation queue is present and empty until the split verb lands")
	assert.Empty(t, status.Consolidations)
}

// TestStatusWithNoVerdictsIsAllPending: a fresh run has decided nothing.
func TestStatusWithNoVerdictsIsAllPending(t *testing.T) {
	run := statusRun(t)

	status := StatusFrom(run, nil)

	assert.Equal(t, len(run.Manifest.Rows), status.Rows)
	assert.Equal(t, len(run.Manifest.Rows), status.Counts[string(RowPendingEvidence)])
	assert.Equal(t, 0, status.Counts[string(RowApproved)])
	assert.True(t, status.Active)
}

// --- row states --------------------------------------------------------

// TestRowStateFromVerdict maps each folded verdict to what status reports.
func TestRowStateFromVerdict(t *testing.T) {
	tests := []struct {
		name  string
		state VerdictState
		want  RowState
	}{
		{"no events yet", VerdictNone, RowPendingEvidence},
		{"proposal awaiting approval", VerdictProposed, RowProposed},
		{"approved", VerdictApproved, RowApproved},
		{"rejected", VerdictRejected, RowRejected},
		{"staled by refresh", VerdictStale, RowStale},
		{"superseded returns the row to the queue", VerdictSuperseded, RowPendingEvidence},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rowStateFor(ManifestRow{}, RowVerdict{State: tc.state})
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestCarriedRowsReportAsCarried: the operator needs to know which decisions
// this run made and which it inherited, because the inherited ones are what a
// changed anchor invalidates.
func TestCarriedRowsReportAsCarried(t *testing.T) {
	row := ManifestRow{CarriedFrom: ptr("run-earlier")}

	assert.Equal(t, RowCarried, rowStateFor(row, RowVerdict{}))
	assert.Equal(t, RowApproved, rowStateFor(row, RowVerdict{State: VerdictApproved}),
		"a verdict recorded in this run wins over the carry")
}

// TestStatusCountsVerdicts folds a real verdict set into the counts.
func TestStatusCountsVerdicts(t *testing.T) {
	run := statusRun(t)
	first := run.Manifest.Rows[0].StableID
	second := run.Manifest.Rows[1].StableID

	status := StatusFrom(run, map[string]RowVerdict{
		first:  {State: VerdictApproved},
		second: {State: VerdictProposed},
	})

	assert.Equal(t, 1, status.Counts[string(RowApproved)])
	assert.Equal(t, 1, status.Counts[string(RowProposed)])
	assert.Equal(t, 0, status.Counts[string(RowPendingEvidence)])
}

// --- batches -----------------------------------------------------------

// TestBatchProgressCountsDecisions: a rejection is decided but not approved,
// so the two counters must not move together.
func TestBatchProgressCountsDecisions(t *testing.T) {
	run := statusRun(t)
	run.Manifest.Rows[0].Batch = 1
	run.Manifest.Rows[1].Batch = 1
	first := run.Manifest.Rows[0].StableID
	second := run.Manifest.Rows[1].StableID

	status := StatusFrom(run, map[string]RowVerdict{
		first:  {State: VerdictApproved},
		second: {State: VerdictRejected},
	})

	require.Len(t, status.Batches, 1)
	assert.Equal(t, 1, status.Batches[0].Batch)
	assert.Equal(t, 2, status.Batches[0].Rows)
	assert.Equal(t, 2, status.Batches[0].Decided)
	assert.Equal(t, 1, status.Batches[0].Approved)
}

// TestBatchesAreReportedInOrder keeps the output stable run to run.
func TestBatchesAreReportedInOrder(t *testing.T) {
	run := statusRun(t)
	run.Manifest.Rows[0].Batch = 7
	run.Manifest.Rows[1].Batch = 2

	status := StatusFrom(run, nil)

	require.Len(t, status.Batches, 2)
	assert.Equal(t, 2, status.Batches[0].Batch)
	assert.Equal(t, 7, status.Batches[1].Batch)
}

// --- closed runs -------------------------------------------------------

// TestStatusReportsAbandonment: an abandoned run still answers, and says why.
func TestStatusReportsAbandonment(t *testing.T) {
	run := statusRun(t)
	run.State.Phase = PhaseAbandoned
	run.State.PhaseHistory = append(run.State.PhaseHistory,
		PhaseTransition{Phase: PhaseAbandoned, At: testAt})
	run.State.AbandonReason = ptr("scope was wrong")

	status := StatusFrom(run, nil)

	assert.False(t, status.Active)
	assert.Equal(t, PhaseAbandoned, status.Phase)
	assert.Equal(t, "scope was wrong", status.AbandonReason)
	assert.Equal(t, len(run.Manifest.Rows), status.Rows,
		"an abandoned run keeps its snapshot")
}

// TestBuildStatusReadsThroughTheStore joins the store to the fold, which is
// the path the command actually takes.
func TestBuildStatusReadsThroughTheStore(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	target := run.Manifest.Rows[0].StableID
	approve := event(DecisionApproved, "completed", "dungeon/completed")
	approve.StableID = target
	require.NoError(t, store.AppendDecision(ctx, run.ID, approve))

	status, err := BuildStatus(ctx, store, run.ID)

	require.NoError(t, err)
	assert.Equal(t, run.ID, status.RunID)
	assert.Equal(t, PhaseCreated, status.Phase)
	assert.Equal(t, len(run.Manifest.Rows), status.Rows)
	assert.Equal(t, 1, status.Counts[string(RowApproved)])
	// The fixture's second row carries a verdict from a base run, so it
	// reports as carried rather than pending.
	assert.Equal(t, 1, status.Counts[string(RowCarried)])
	assert.Equal(t, 0, status.Counts[string(RowPendingEvidence)])
}

// TestBuildStatusOnMissingRun reports not-found rather than an empty status.
func TestBuildStatusOnMissingRun(t *testing.T) {
	store, _ := newTestStore(t)

	_, err := BuildStatus(context.Background(), store, "run-19700101T000000Z")

	assert.Error(t, err)
}

// TestBuildStatusRespectsCancellation stops before reading.
func TestBuildStatusRespectsCancellation(t *testing.T) {
	store, _ := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := BuildStatus(ctx, store, "run-1")

	assert.ErrorIs(t, err, context.Canceled)
}

// TestStatusReportsIdentityExceptions surfaces rows that triage by path only.
func TestStatusReportsIdentityExceptions(t *testing.T) {
	run := statusRun(t)
	run.Manifest.Rows[0].IdentityException = &IdentityException{
		Reason:    "no marker",
		Path:      "workflow/design/legacy",
		GrantedBy: "camp-triage-preflight",
		GrantedAt: testAt,
	}

	status := StatusFrom(run, nil)

	assert.Equal(t, 1, status.IdentityIssues)
}

// TestConsolidationQueue is the one derivation status, the review card, and
// verify all read. A second implementation of "which successors are missing"
// would eventually disagree, and the disagreement would surface as a parent
// status calls ready and the gate refuses to retire.
func TestConsolidationQueue(t *testing.T) {
	rows := []ManifestRow{
		{StableID: "design-umbrella", Type: "design"},
		{StableID: "design-parked", Type: "design"},
	}

	tests := []struct {
		name        string
		verdicts    map[string]RowVerdict
		successors  map[string][]string
		discovered  map[string]bool
		wantLen     int
		wantMissing []string
		wantBlocked bool
	}{
		{
			name: "a consolidation with every successor present is unblocked",
			verdicts: map[string]RowVerdict{
				"design-umbrella": {CanonicalAction: ActionSplit},
			},
			successors:  map[string][]string{"design-umbrella": {"part-b", "part-a"}},
			discovered:  map[string]bool{"part-a": true, "part-b": true},
			wantLen:     1,
			wantMissing: []string{},
		},
		{
			name: "a missing successor blocks the parent",
			verdicts: map[string]RowVerdict{
				"design-umbrella": {CanonicalAction: ActionSplit},
			},
			successors:  map[string][]string{"design-umbrella": {"part-a", "part-b"}},
			discovered:  map[string]bool{"part-a": true},
			wantLen:     1,
			wantMissing: []string{"part-b"},
			wantBlocked: true,
		},
		{
			name: "non-consolidate verdicts are not in the queue",
			verdicts: map[string]RowVerdict{
				"design-parked": {CanonicalAction: CanonicalAction("attention/parked")},
			},
		},
		{
			name: "an undecided row is not in the queue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConsolidationQueue(ConsolidationInput{
				Rows: rows, Verdicts: tt.verdicts,
				Successors: tt.successors, Discovered: tt.discovered,
			})
			require.Len(t, got, tt.wantLen)
			if tt.wantLen == 0 {
				return
			}
			assert.Equal(t, "design-umbrella", got[0].StableID)
			assert.Equal(t, tt.wantMissing, got[0].Missing)
			assert.Equal(t, tt.wantBlocked, got[0].Blocked())
			assert.Equal(t, []string{"part-a", "part-b"}, got[0].Successors,
				"successors are sorted so the queue reads the same every time")
		})
	}
}

// TestConsolidationQueueIsPure guards the reuse rule.
func TestConsolidationQueueIsPure(t *testing.T) {
	successors := map[string][]string{"design-umbrella": {"part-b", "part-a"}}
	ConsolidationQueue(ConsolidationInput{
		Rows:       []ManifestRow{{StableID: "design-umbrella"}},
		Verdicts:   map[string]RowVerdict{"design-umbrella": {CanonicalAction: ActionSplit}},
		Successors: successors,
		Discovered: map[string]bool{},
	})
	assert.Equal(t, []string{"part-b", "part-a"}, successors["design-umbrella"],
		"the caller's successor list must not be sorted in place")
}

// TestConsolidationQueueNeverReturnsNilSlices keeps the JSON contract free of
// nulls, which break naive consumers.
func TestConsolidationQueueNeverReturnsNilSlices(t *testing.T) {
	got := ConsolidationQueue(ConsolidationInput{})
	assert.NotNil(t, got)
	assert.Empty(t, got)

	withRow := ConsolidationQueue(ConsolidationInput{
		Rows:     []ManifestRow{{StableID: "x"}},
		Verdicts: map[string]RowVerdict{"x": {CanonicalAction: ActionSplit}},
	})
	require.Len(t, withRow, 1)
	assert.NotNil(t, withRow[0].Successors)
	assert.NotNil(t, withRow[0].Missing)
}

// TestStatusReportsCarryLossesFrozenAtStart is spec doc 04's requirement that
// status answer why a row is being judged again.
//
// A row re-queued when the run was built holds no verdict and is not marked
// carried, so nothing about the row itself records the reason. Without the
// manifest's own list, the only place that reason ever existed was the start
// command's output, and an operator asking "why am I looking at this again"
// is by definition asking after that output is gone.
func TestStatusReportsCarryLossesFrozenAtStart(t *testing.T) {
	run := statusRun(t)
	run.Manifest.CarryLosses = []CarryLoss{
		{StableID: "design-observation-boundary", Reason: "the base run's proposal was never approved"},
	}

	status := StatusFrom(run, nil)

	require.Len(t, status.CarryLosses, 1)
	assert.Equal(t, "design-observation-boundary", status.CarryLosses[0].StableID)
	assert.Contains(t, status.CarryLosses[0].Reason, "never approved")
}

// TestStatusCarryLossesAreSorted keeps two reads of an unchanged run in
// agreement, so a diff of two payloads shows only what moved.
func TestStatusCarryLossesAreSorted(t *testing.T) {
	run := statusRun(t)
	run.Manifest.CarryLosses = []CarryLoss{
		{StableID: "zzz-last", Reason: "classified changed"},
		{StableID: "aaa-first", Reason: "classified gone"},
	}

	status := StatusFrom(run, nil)

	require.Len(t, status.CarryLosses, 2)
	assert.Equal(t, "aaa-first", status.CarryLosses[0].StableID)
	assert.Equal(t, "zzz-last", status.CarryLosses[1].StableID)
}
