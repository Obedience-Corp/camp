package triage

import (
	"context"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rationale(summary string) *Rationale {
	return &Rationale{
		SchemaVersion: SchemaVersion,
		Summary:       summary,
		Confidence:    ConfidenceMedium,
	}
}

// proposeStore returns a store with a run whose rows all hold evidence, so
// proposing is the only thing left to do.
func proposeStore(t *testing.T) (*Store, *Run) {
	t.Helper()
	ctx := context.Background()
	store, _ := newTestStore(t)
	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)
	for _, row := range run.Manifest.Rows {
		record := validEvidence()
		record.StableID = row.StableID
		_, err := store.WriteEvidence(ctx, run.ID, record)
		require.NoError(t, err)
	}
	return store, run
}

// --- disposition resolution --------------------------------------------

// TestResolveDispositionRejectsAnUnknownLabel: the label is the only thing the
// operator typed, so guessing at it would move real work to the wrong place.
func TestResolveDispositionRejectsAnUnknownLabel(t *testing.T) {
	tests := []struct {
		name    string
		wfType  string
		label   string
		wantAny []string
	}{
		{
			name:    "label from another type's vocabulary",
			wfType:  "intent",
			label:   "consolidate",
			wantAny: []string{"promote", "park", "drop"},
		},
		{
			name:    "invented label",
			wfType:  "design",
			label:   "shelve",
			wantAny: []string{"completed", "parked"},
		},
		{
			name:    "empty label",
			wfType:  "design",
			label:   "",
			wantAny: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveDisposition(TypePolicyFor(tc.wfType), tc.label)

			require.Error(t, err)
			require.ErrorIs(t, err, camperrors.ErrInvalidInput)
			for _, want := range tc.wantAny {
				assert.Contains(t, err.Error(), want,
					"the refusal must list the vocabulary that would have worked")
			}
		})
	}
}

// TestResolveDispositionMapsLabelsToActions covers the default map and the
// per-type vocabularies spec doc 05 ships.
func TestResolveDispositionMapsLabelsToActions(t *testing.T) {
	tests := []struct {
		wfType string
		label  string
		want   CanonicalAction
	}{
		{"design", "completed", "dungeon/completed"},
		{"design", "consolidate", ActionSplit},
		{"design", "parked", "attention/parked"},
		{"design", "next", "attention/next"},
		{"design", "ready", "rail/ready"},
		{"intent", "promote", "rail/ready"},
		{"intent", "drop", "dungeon/archived"},
		{"intent", "keep", ActionNone},
		{"festival", "completed", "dungeon/completed"},
		// A type camp has never heard of inherits the default vocabulary, so
		// a campaign that invents one can triage it with zero configuration.
		{"invented-type", "completed", "dungeon/completed"},
		{"invented-type", "consolidate", ActionSplit},
	}

	for _, tc := range tests {
		t.Run(tc.wfType+"/"+tc.label, func(t *testing.T) {
			action, err := ResolveDisposition(TypePolicyFor(tc.wfType), tc.label)

			require.NoError(t, err)
			assert.Equal(t, tc.want, action)
		})
	}
}

// TestCustomLabelResolvesToACampAction is the indirection's whole point: a
// campaign renames the label, camp still performs its own action.
func TestCustomLabelResolvesToACampAction(t *testing.T) {
	policy := TypePolicy{
		SchemaVersion: TypePolicySchemaVersion,
		Dispositions: map[string]CanonicalAction{
			"concluded": "dungeon/completed",
			"on-ice":    "attention/parked",
		},
	}

	action, err := ResolveDisposition(policy, "concluded")
	require.NoError(t, err)
	assert.Equal(t, CanonicalAction("dungeon/completed"), action)

	_, err = ResolveDisposition(policy, "completed")
	require.Error(t, err, "a renamed vocabulary no longer offers the old label")
	assert.Contains(t, err.Error(), "concluded")
	assert.Contains(t, err.Error(), "on-ice")
}

// TestEveryBuiltinDispositionResolvesToARealAction is a drift guard: a policy
// naming an action camp cannot perform would fail at apply time, long after
// the operator chose it.
func TestEveryBuiltinDispositionResolvesToARealAction(t *testing.T) {
	valid := map[string]bool{}
	for _, action := range CanonicalActions() {
		valid[action] = true
	}

	for _, wfType := range []string{"design", "explore", "intent", "festival", "unknown"} {
		policy := TypePolicyFor(wfType)
		require.NotEmpty(t, policy.Dispositions, "type %s has no vocabulary", wfType)
		for label, action := range policy.Dispositions {
			assert.True(t, valid[string(action)],
				"type %s label %s maps to %s, which camp cannot perform", wfType, label, action)
		}
	}
}

// --- rationale ---------------------------------------------------------

// TestRationaleRejectsAnEmptySummary: a proposal whose reasoning is blank
// cannot be reviewed, only trusted.
func TestRationaleRejectsAnEmptySummary(t *testing.T) {
	r := &Rationale{SchemaVersion: SchemaVersion, Confidence: ConfidenceHigh}

	assert.Contains(t, fieldsOf(r.Validate()), "summary")
}

// TestRationaleRejectsAnUnknownConfidence keeps the vocabulary shared with
// evidence records.
func TestRationaleRejectsAnUnknownConfidence(t *testing.T) {
	r := &Rationale{SchemaVersion: SchemaVersion, Summary: "because", Confidence: "certain"}

	violations := r.Validate()

	require.Len(t, violations, 1)
	assert.Equal(t, "confidence", violations[0].Field)
	assert.Equal(t, Confidences(), violations[0].Allowed)
}

// TestRationaleRoundTrips keeps it storable like every other document.
func TestRationaleRoundTrips(t *testing.T) {
	r := &Rationale{
		SchemaVersion: SchemaVersion,
		Summary:       "delivered in PR 239; nothing left open",
		AnchorsUsed:   []string{"pr:obey#239", "path:projects/camp"},
		Confidence:    ConfidenceHigh,
	}

	first, err := MarshalDocument(r)
	require.NoError(t, err)
	var again Rationale
	require.NoError(t, ParseDocument(first, &again, Strict))
	second, err := MarshalDocument(&again)
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second))
	assert.Equal(t, r.AnchorsUsed, again.AnchorsUsed)
}

// --- propose -----------------------------------------------------------

// TestProposeRejectsAnUnknownRow names the id rather than recording a verdict
// for something not in the run.
func TestProposeRejectsAnUnknownRow(t *testing.T) {
	store, run := proposeStore(t)

	_, err := store.Propose(context.Background(), ProposeInput{
		RunID: run.ID, StableID: "no-such-row", Disposition: "parked",
		Rationale: rationale("x"), Actor: "tester", Now: testAt,
	})

	require.Error(t, err)
	var notFound *camperrors.NotFoundError
	assert.True(t, camperrors.As(err, &notFound))
	assert.Contains(t, err.Error(), "no-such-row")
}

// TestProposeRequiresARationaleAndAnActor: an unattributed or unexplained
// verdict cannot be questioned later, which defeats recorded judgment.
func TestProposeRequiresARationaleAndAnActor(t *testing.T) {
	store, run := proposeStore(t)
	target := run.Manifest.Rows[0].StableID

	_, err := store.Propose(context.Background(), ProposeInput{
		RunID: run.ID, StableID: target, Disposition: "parked", Actor: "tester", Now: testAt,
	})
	assert.ErrorIs(t, err, camperrors.ErrInvalidInput)

	_, err = store.Propose(context.Background(), ProposeInput{
		RunID: run.ID, StableID: target, Disposition: "parked",
		Rationale: rationale("x"), Now: testAt,
	})
	assert.ErrorIs(t, err, camperrors.ErrInvalidInput)
}

// TestProposeRecordsTheActionNotJustTheLabel is what makes apply mechanical.
func TestProposeRecordsTheActionNotJustTheLabel(t *testing.T) {
	ctx := context.Background()
	store, run := proposeStore(t)
	target := run.Manifest.Rows[0].StableID

	result, err := store.Propose(ctx, ProposeInput{
		RunID: run.ID, StableID: target, Disposition: "completed",
		Rationale: rationale("shipped in PR 239"), Actor: "lancekrogers", Now: testAt,
	})

	require.NoError(t, err)
	assert.Equal(t, CanonicalAction("dungeon/completed"), result.CanonicalAction)
	assert.True(t, result.RequiresApproval, "a dungeon move is terminal")
	assert.Contains(t, result.RationaleRef, RationaleDirName)

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, VerdictProposed, verdicts[target].State)
	assert.Equal(t, CanonicalAction("dungeon/completed"), verdicts[target].CanonicalAction)
	assert.Equal(t, "lancekrogers", verdicts[target].Actor)

	stored, err := store.Evidence(ctx, run.ID, target)
	require.NoError(t, err)
	require.NotNil(t, stored, "proposing must not disturb the evidence")
}

// TestNonTerminalProposalDoesNotRequireApproval: attention changes are
// recoverable, so they are not gated the way a dungeon move is.
func TestNonTerminalProposalDoesNotRequireApproval(t *testing.T) {
	store, run := proposeStore(t)

	result, err := store.Propose(context.Background(), ProposeInput{
		RunID: run.ID, StableID: run.Manifest.Rows[0].StableID, Disposition: "parked",
		Rationale: rationale("not now"), Actor: "tester", Now: testAt,
	})

	require.NoError(t, err)
	assert.Equal(t, CanonicalAction("attention/parked"), result.CanonicalAction)
	assert.False(t, result.RequiresApproval)
}

// TestReproposingSupersedesWithFullProvenance is the requirement that nothing
// is overwritten: the stream keeps the whole argument.
func TestReproposingSupersedesWithFullProvenance(t *testing.T) {
	ctx := context.Background()
	store, run := proposeStore(t)
	target := run.Manifest.Rows[0].StableID

	_, err := store.Propose(ctx, ProposeInput{
		RunID: run.ID, StableID: target, Disposition: "completed",
		Rationale: rationale("looks shipped"), Actor: "tester", Now: testAt,
	})
	require.NoError(t, err)

	second, err := store.Propose(ctx, ProposeInput{
		RunID: run.ID, StableID: target, Disposition: "parked",
		Rationale: rationale("actually, revisit later"), Actor: "tester", Now: testAt.Add(60),
	})
	require.NoError(t, err)
	assert.Equal(t, "completed", second.Superseded)

	events, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, events, 3, "proposed, superseded, proposed")
	assert.Equal(t, DecisionProposed, events[0].Event)
	assert.Equal(t, "completed", events[0].Disposition)
	assert.Equal(t, DecisionSuperseded, events[1].Event)
	assert.Equal(t, "completed", events[1].Disposition,
		"the retired proposal is named, not blanked")
	assert.Equal(t, DecisionProposed, events[2].Event)
	assert.Equal(t, "parked", events[2].Disposition)

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, VerdictProposed, verdicts[target].State)
	assert.Equal(t, "parked", verdicts[target].Disposition, "the live proposal is the newest")
}

// TestFirstProposalSupersedesNothing keeps the stream free of an empty
// retirement event.
func TestFirstProposalSupersedesNothing(t *testing.T) {
	ctx := context.Background()
	store, run := proposeStore(t)

	result, err := store.Propose(ctx, ProposeInput{
		RunID: run.ID, StableID: run.Manifest.Rows[0].StableID, Disposition: "parked",
		Rationale: rationale("first"), Actor: "tester", Now: testAt,
	})

	require.NoError(t, err)
	assert.Empty(t, result.Superseded)
	events, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)
}
