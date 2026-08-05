package triage

import (
	"context"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// queueStore returns a store holding one run whose rows are all fresh.
func queueStore(t *testing.T) (*Store, *Run) {
	t.Helper()
	store, _ := newTestStore(t)
	manifest := newManifestForStore()
	// Drop the fixture's carried row so the default case is "everything is
	// waiting"; the carry behavior gets its own test.
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(context.Background(), manifest)
	require.NoError(t, err)
	return store, run
}

func evidenceFor(stableID string) *EvidenceRecord {
	record := validEvidence()
	record.StableID = stableID
	return record
}

// --- error cases first -------------------------------------------------

// TestBuildQueueRejectsAnUnknownRole: a mistyped role must not silently return
// everything, which would look like the filter worked.
func TestBuildQueueRejectsAnUnknownRole(t *testing.T) {
	store, run := queueStore(t)

	_, err := BuildQueue(context.Background(), store, run.ID, QueueRole("reviewer"))

	require.Error(t, err)
	require.ErrorIs(t, err, camperrors.ErrInvalidInput)
	assert.Contains(t, violatedFields(err), "role")
	assert.Contains(t, err.Error(), "evidence")
	assert.Contains(t, err.Error(), "synthesis")
}

// TestBuildQueueOnMissingRun reports not-found rather than an empty queue.
func TestBuildQueueOnMissingRun(t *testing.T) {
	store, _ := queueStore(t)

	_, err := BuildQueue(context.Background(), store, "run-19700101T000000Z", "")

	assert.Error(t, err)
}

// TestBuildQueueRespectsCancellation stops before reading.
func TestBuildQueueRespectsCancellation(t *testing.T) {
	store, run := queueStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := BuildQueue(ctx, store, run.ID, "")

	assert.ErrorIs(t, err, context.Canceled)
}

// --- roles -------------------------------------------------------------

// TestQueueStartsEverythingAwaitingEvidence: a fresh run has read nothing.
func TestQueueStartsEverythingAwaitingEvidence(t *testing.T) {
	store, run := queueStore(t)

	queue, err := BuildQueue(context.Background(), store, run.ID, "")

	require.NoError(t, err)
	require.Len(t, queue.Items, len(run.Manifest.Rows))
	for _, item := range queue.Items {
		assert.Equal(t, QueueRoleEvidence, item.Role)
	}
	assert.Equal(t, len(run.Manifest.Rows), queue.Counts.Evidence)
	assert.Equal(t, 0, queue.Counts.Synthesis)
	assert.Equal(t, 0, queue.Counts.Done)
	assert.Equal(t, SchemaVersion, queue.EvidenceSchemaVersion)
}

// TestEvidenceMovesARowToSynthesis: once a row has been read, what it needs
// next is a disposition, not another reading.
func TestEvidenceMovesARowToSynthesis(t *testing.T) {
	ctx := context.Background()
	store, run := queueStore(t)
	target := run.Manifest.Rows[0].StableID
	_, err := store.WriteEvidence(ctx, run.ID, evidenceFor(target))
	require.NoError(t, err)

	queue, err := BuildQueue(ctx, store, run.ID, "")

	require.NoError(t, err)
	byID := map[string]QueueRole{}
	for _, item := range queue.Items {
		byID[item.StableID] = item.Role
	}
	assert.Equal(t, QueueRoleSynthesis, byID[target])
	assert.Equal(t, 1, queue.Counts.Synthesis)
	assert.Equal(t, len(run.Manifest.Rows)-1, queue.Counts.Evidence)
}

// TestProposedRowLeavesTheQueue: a row holding a proposal needs a human to
// approve it, which is the review surface's business rather than a driver's.
func TestProposedRowLeavesTheQueue(t *testing.T) {
	ctx := context.Background()
	store, run := queueStore(t)
	target := run.Manifest.Rows[0].StableID
	_, err := store.WriteEvidence(ctx, run.ID, evidenceFor(target))
	require.NoError(t, err)

	proposal := event(DecisionProposed, "completed", "dungeon/completed")
	proposal.StableID = target
	require.NoError(t, store.AppendDecision(ctx, run.ID, proposal))

	queue, err := BuildQueue(ctx, store, run.ID, "")

	require.NoError(t, err)
	for _, item := range queue.Items {
		assert.NotEqual(t, target, item.StableID, "a proposed row is not queued")
	}
	assert.Equal(t, 1, queue.Counts.Done)
}

// TestCarriedRowIsNotQueued: its verdict came forward precisely so nobody
// re-reads it.
func TestCarriedRowIsNotQueued(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	manifest := newManifestForStore()
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)
	carried := run.Manifest.Rows[1]
	require.NotNil(t, carried.CarriedFrom)

	queue, err := BuildQueue(ctx, store, run.ID, "")

	require.NoError(t, err)
	for _, item := range queue.Items {
		assert.NotEqual(t, carried.StableID, item.StableID)
	}
	assert.Equal(t, 1, queue.Counts.Done)
}

// --- filtering ---------------------------------------------------------

// TestQueueRoleFilterNarrowsItemsButNotCounts: a driver asking for its own
// slice still needs to see the whole run's progress.
func TestQueueRoleFilterNarrowsItemsButNotCounts(t *testing.T) {
	ctx := context.Background()
	store, run := queueStore(t)
	target := run.Manifest.Rows[0].StableID
	_, err := store.WriteEvidence(ctx, run.ID, evidenceFor(target))
	require.NoError(t, err)

	synthesis, err := BuildQueue(ctx, store, run.ID, QueueRoleSynthesis)
	require.NoError(t, err)
	require.Len(t, synthesis.Items, 1)
	assert.Equal(t, target, synthesis.Items[0].StableID)
	assert.Equal(t, len(run.Manifest.Rows)-1, synthesis.Counts.Evidence,
		"counts describe the run, not the filter")

	evidence, err := BuildQueue(ctx, store, run.ID, QueueRoleEvidence)
	require.NoError(t, err)
	assert.Len(t, evidence.Items, len(run.Manifest.Rows)-1)
}

// --- payload -----------------------------------------------------------

// TestQueueCarriesEverythingADriverNeeds: the point of the queue is that a
// driver does not have to ask camp a second question to start work.
func TestQueueCarriesEverythingADriverNeeds(t *testing.T) {
	store, run := queueStore(t)

	queue, err := BuildQueue(context.Background(), store, run.ID, "")

	require.NoError(t, err)
	require.NotEmpty(t, queue.Items)
	item := queue.Items[0]
	assert.NotEmpty(t, item.StableID)
	assert.NotEmpty(t, item.Type)
	assert.NotEmpty(t, item.RelativePath)
	assert.GreaterOrEqual(t, item.Batch, 1)
	assert.NotEmpty(t, item.Policy.Evidence, "how much evidence to gather")
	assert.NotEmpty(t, item.Policy.RoutingTier, "and which class to read it with")

	assert.Equal(t, run.Manifest.Profile.Resolved.Routing, queue.Routing,
		"the routing block is passed through verbatim")
	assert.Equal(t, run.State.Phase, queue.Phase)
}

// TestQueueItemsAreNeverNull keeps the JSON shape stable for consumers.
func TestQueueItemsAreNeverNull(t *testing.T) {
	ctx := context.Background()
	store, run := queueStore(t)
	for _, row := range run.Manifest.Rows {
		_, err := store.WriteEvidence(ctx, run.ID, evidenceFor(row.StableID))
		require.NoError(t, err)
		proposal := event(DecisionProposed, "parked", "attention/parked")
		proposal.StableID = row.StableID
		require.NoError(t, store.AppendDecision(ctx, run.ID, proposal))
	}

	queue, err := BuildQueue(ctx, store, run.ID, "")

	require.NoError(t, err)
	assert.NotNil(t, queue.Items)
	assert.Empty(t, queue.Items)
	assert.Equal(t, len(run.Manifest.Rows), queue.Counts.Done)
}

// --- store helper ------------------------------------------------------

// TestHasEvidenceDoesNotParse: the queue asks this once per row, so it must
// stay a stat rather than a read.
func TestHasEvidenceDoesNotParse(t *testing.T) {
	ctx := context.Background()
	store, run := queueStore(t)
	target := run.Manifest.Rows[0].StableID

	has, err := store.HasEvidence(ctx, run.ID, target)
	require.NoError(t, err)
	assert.False(t, has)

	_, err = store.WriteEvidence(ctx, run.ID, evidenceFor(target))
	require.NoError(t, err)

	has, err = store.HasEvidence(ctx, run.ID, target)
	require.NoError(t, err)
	assert.True(t, has)
}
