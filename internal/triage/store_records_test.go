package triage

import (
	"context"
	"path/filepath"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- evidence ----------------------------------------------------------

// TestWriteEvidenceIsIdempotentByContent: a driver retrying a batch must not
// churn the run, but a genuinely revised record must land.
func TestWriteEvidenceIsIdempotentByContent(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	written, err := store.WriteEvidence(ctx, run.ID, validEvidence())
	require.NoError(t, err)
	assert.True(t, written, "first submission writes")

	written, err = store.WriteEvidence(ctx, run.ID, validEvidence())
	require.NoError(t, err)
	assert.False(t, written, "an identical resubmission changes nothing")

	revised := validEvidence()
	revised.Confidence = ConfidenceHigh
	written, err = store.WriteEvidence(ctx, run.ID, revised)
	require.NoError(t, err)
	assert.True(t, written, "different content supersedes")

	stored, err := store.Evidence(ctx, run.ID, revised.StableID)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, ConfidenceHigh, stored.Confidence)
}

// TestWriteEvidenceRejectsInvalidRecord: validation happens before the write,
// so a bad record never reaches the run.
func TestWriteEvidenceRejectsInvalidRecord(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)
	record := validEvidence()
	record.Confidence = "certain"

	written, err := store.WriteEvidence(ctx, run.ID, record)

	assert.False(t, written)
	require.ErrorIs(t, err, camperrors.ErrInvalidInput)
	stored, err := store.Evidence(ctx, run.ID, record.StableID)
	require.NoError(t, err)
	assert.Nil(t, stored)
}

// TestEvidenceMissingIsNotAnError: a row may legitimately have no record yet.
func TestEvidenceMissingIsNotAnError(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	stored, err := store.Evidence(ctx, run.ID, "never-written")

	require.NoError(t, err)
	assert.Nil(t, stored)
}

// TestEvidenceFileNameContainsPathTraversal: stable ids come from markers camp
// did not necessarily write, so a hostile or malformed id must not escape the
// evidence directory. Ids that need no rewriting keep a plain readable name;
// anything sanitized carries a digest of the original.
func TestEvidenceFileNameContainsPathTraversal(t *testing.T) {
	tests := []struct {
		name     string
		stableID string
		want     string
	}{
		{"clean slug is untouched", "design-thing", "design-thing.json"},
		{"underscores are allowed", "design_thing_2", "design_thing_2.json"},
		{"parent traversal", "../../etc/passwd", "------etc-passwd-3754d6cb.json"},
		{"path separator", "a/b", "a-b-c14cddc0.json"},
		{"empty id", "", "unnamed-e3b0c442.json"},
		{"dots only", "...", "unnamed-ab5df625.json"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name := recordFileName(tc.stableID)
			assert.Equal(t, tc.want, name)
			assert.Equal(t, name, filepath.Base(name), "must stay a bare filename")
		})
	}
}

// TestEvidenceFileNameIsInjective is the finding this digest exists for:
// sanitizing is lossy, so without it "a/b" and "a-b" would share one evidence
// file and one row's findings would silently overwrite another's.
func TestEvidenceFileNameIsInjective(t *testing.T) {
	colliding := []string{"a/b", "a-b", "a.b", "a b", "a:b"}

	seen := make(map[string]string, len(colliding))
	for _, id := range colliding {
		name := recordFileName(id)
		previous, clash := seen[name]
		assert.False(t, clash, "%q and %q both map to %s", previous, id, name)
		seen[name] = id
	}
	assert.Len(t, seen, len(colliding))
}

// TestWriteEvidenceKeepsDistinctRowsApart proves the same thing through the
// store, not just the naming function.
func TestWriteEvidenceKeepsDistinctRowsApart(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	first := validEvidence()
	first.StableID = "a/b"
	first.OriginalGoal = "first row"
	second := validEvidence()
	second.StableID = "a-b"
	second.OriginalGoal = "second row"

	_, err = store.WriteEvidence(ctx, run.ID, first)
	require.NoError(t, err)
	_, err = store.WriteEvidence(ctx, run.ID, second)
	require.NoError(t, err)

	storedFirst, err := store.Evidence(ctx, run.ID, "a/b")
	require.NoError(t, err)
	require.NotNil(t, storedFirst)
	storedSecond, err := store.Evidence(ctx, run.ID, "a-b")
	require.NoError(t, err)
	require.NotNil(t, storedSecond)

	assert.Equal(t, "first row", storedFirst.OriginalGoal)
	assert.Equal(t, "second row", storedSecond.OriginalGoal)
}

// --- decisions and the fold --------------------------------------------

// TestAppendDecisionRoundTrips proves the stream survives the write path in
// order.
func TestAppendDecisionRoundTrips(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	for _, disposition := range []string{"parked", "next", "completed"} {
		event := validDecision()
		event.Event = DecisionProposed
		event.Disposition = disposition
		event.CanonicalAction = "attention/parked"
		require.NoError(t, store.AppendDecision(ctx, run.ID, *event))
	}

	events, err := store.Decisions(ctx, run.ID)

	require.NoError(t, err)
	require.Len(t, events, 3)
	assert.Equal(t, "parked", events[0].Disposition)
	assert.Equal(t, "completed", events[2].Disposition)
}

// TestDecisionsOnEmptyRun returns nothing rather than failing: a run before
// any judgment is a normal state.
func TestDecisionsOnEmptyRun(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	events, err := store.Decisions(ctx, run.ID)

	require.NoError(t, err)
	assert.Empty(t, events)
}

// TestFoldDecisions covers the chains the review and apply phases depend on,
// including the approved -> stale -> re-approved cycle a refresh produces.
func TestFoldDecisions(t *testing.T) {
	tests := []struct {
		name       string
		events     []DecisionEvent
		wantState  VerdictState
		wantDisp   string
		wantAction CanonicalAction
		wantEvents int
		applicable bool
	}{
		{
			name:       "proposal awaiting approval",
			events:     []DecisionEvent{event(DecisionProposed, "completed", "dungeon/completed")},
			wantState:  VerdictProposed,
			wantDisp:   "completed",
			wantAction: "dungeon/completed",
			wantEvents: 1,
		},
		{
			name: "approved proposal",
			events: []DecisionEvent{
				event(DecisionProposed, "completed", "dungeon/completed"),
				event(DecisionApproved, "completed", "dungeon/completed"),
			},
			wantState:  VerdictApproved,
			wantDisp:   "completed",
			wantAction: "dungeon/completed",
			wantEvents: 2,
			applicable: true,
		},
		{
			name: "amendment replaces the proposed disposition",
			events: []DecisionEvent{
				event(DecisionProposed, "completed", "dungeon/completed"),
				event(DecisionAmended, "parked", "attention/parked"),
			},
			wantState:  VerdictApproved,
			wantDisp:   "parked",
			wantAction: "attention/parked",
			wantEvents: 2,
			applicable: true,
		},
		{
			name: "rejection re-queues the row",
			events: []DecisionEvent{
				event(DecisionProposed, "archived", "dungeon/archived"),
				event(DecisionRejected, "archived", "dungeon/archived"),
			},
			wantState:  VerdictRejected,
			wantDisp:   "archived",
			wantAction: "dungeon/archived",
			wantEvents: 2,
		},
		{
			name: "a newer proposal supersedes the last",
			events: []DecisionEvent{
				event(DecisionProposed, "completed", "dungeon/completed"),
				event(DecisionSuperseded, "", ""),
				event(DecisionProposed, "parked", "attention/parked"),
			},
			wantState:  VerdictProposed,
			wantDisp:   "parked",
			wantAction: "attention/parked",
			wantEvents: 3,
		},
		{
			name: "superseded with no replacement yet keeps what it retired",
			events: []DecisionEvent{
				event(DecisionProposed, "completed", "dungeon/completed"),
				event(DecisionSuperseded, "", ""),
			},
			wantState:  VerdictSuperseded,
			wantDisp:   "completed",
			wantAction: "dungeon/completed",
			wantEvents: 2,
		},
		{
			name: "refresh stales an approval and keeps what went stale",
			events: []DecisionEvent{
				event(DecisionProposed, "completed", "dungeon/completed"),
				event(DecisionApproved, "completed", "dungeon/completed"),
				event(DecisionStale, "", ""),
			},
			wantState:  VerdictStale,
			wantDisp:   "completed",
			wantAction: "dungeon/completed",
			wantEvents: 3,
		},
		{
			name: "approved then staled then re-approved",
			events: []DecisionEvent{
				event(DecisionProposed, "completed", "dungeon/completed"),
				event(DecisionApproved, "completed", "dungeon/completed"),
				event(DecisionStale, "", ""),
				event(DecisionProposed, "archived", "dungeon/archived"),
				event(DecisionApproved, "archived", "dungeon/archived"),
			},
			wantState:  VerdictApproved,
			wantDisp:   "archived",
			wantAction: "dungeon/archived",
			wantEvents: 5,
			applicable: true,
		},
		{
			name: "approved but nothing to do is not applicable",
			events: []DecisionEvent{
				event(DecisionProposed, "keep", string(ActionNone)),
				event(DecisionApproved, "keep", string(ActionNone)),
			},
			wantState:  VerdictApproved,
			wantDisp:   "keep",
			wantAction: ActionNone,
			wantEvents: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			verdicts := FoldDecisions(tc.events)

			require.Len(t, verdicts, 1)
			got := verdicts["row-1"]
			assert.Equal(t, "row-1", got.StableID)
			assert.Equal(t, tc.wantState, got.State)
			assert.Equal(t, tc.wantDisp, got.Disposition)
			assert.Equal(t, tc.wantAction, got.CanonicalAction)
			assert.Equal(t, tc.wantEvents, got.Events)
			assert.Equal(t, tc.applicable, got.Applicable())
		})
	}
}

// TestFoldDecisionsSeparatesRows: one stream carries every row, so the fold
// must not let one row's events leak into another's verdict.
func TestFoldDecisionsSeparatesRows(t *testing.T) {
	first := event(DecisionApproved, "completed", "dungeon/completed")
	second := event(DecisionProposed, "parked", "attention/parked")
	second.StableID = "row-2"

	verdicts := FoldDecisions([]DecisionEvent{first, second})

	require.Len(t, verdicts, 2)
	assert.Equal(t, VerdictApproved, verdicts["row-1"].State)
	assert.Equal(t, VerdictProposed, verdicts["row-2"].State)
}

// TestFoldDecisionsOnEmptyStream returns an empty map, not nil, so callers can
// range over it without a guard.
func TestFoldDecisionsOnEmptyStream(t *testing.T) {
	verdicts := FoldDecisions(nil)

	assert.NotNil(t, verdicts)
	assert.Empty(t, verdicts)
}

// TestVerdictsReadsThroughTheStore joins the append path to the fold.
func TestVerdictsReadsThroughTheStore(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	run, err := store.CreateRun(ctx, newManifestForStore())
	require.NoError(t, err)

	require.NoError(t, store.AppendDecision(ctx, run.ID, event(DecisionProposed, "completed", "dungeon/completed")))
	require.NoError(t, store.AppendDecision(ctx, run.ID, event(DecisionApproved, "completed", "dungeon/completed")))

	verdicts, err := store.Verdicts(ctx, run.ID)

	require.NoError(t, err)
	require.Contains(t, verdicts, "row-1")
	assert.True(t, verdicts["row-1"].Applicable())
}

// event builds a decision event for the shared fixture row.
func event(kind DecisionEventKind, disposition, action string) DecisionEvent {
	return DecisionEvent{
		SchemaVersion:   SchemaVersion,
		Event:           kind,
		StableID:        "row-1",
		Disposition:     disposition,
		CanonicalAction: CanonicalAction(action),
		Actor:           "lancekrogers",
		At:              testAt,
	}
}
