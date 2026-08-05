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
