package triage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// carryFixture builds the two-run setup every case here perturbs: one row,
// judged in the base run, snapshotted again into a new one.
func carryFixture(state VerdictState) CarryForwardInput {
	row := diffRow()
	base := row

	return CarryForwardInput{
		BaseRunID: "run-20260803T234110Z",
		Rows:      []ManifestRow{row},
		BaseRows:  []ManifestRow{base},
		BaseVerdicts: map[string]RowVerdict{
			row.StableID: {
				StableID:        row.StableID,
				State:           state,
				Disposition:     "parked",
				CanonicalAction: CanonicalAction("attention/parked"),
				Events:          2,
			},
		},
		Classes:      map[string]RowClass{row.StableID: ClassFresh},
		BaseProfile:  normalizedDefault(),
		NextProfile:  normalizedDefault(),
		BasePolicies: map[string]TypePolicy{},
		NextPolicies: map[string]TypePolicy{},
	}
}

func normalizedDefault() ResolvedProfile {
	p := DefaultProfile()
	p.Normalize()
	return p
}

// TestCarryForwardCarriesAnApprovedVerdict is D4's whole point: the second
// triage of a campaign should be small, so a row nothing touched must come
// forward marked with the run that decided it.
func TestCarryForwardCarriesAnApprovedVerdict(t *testing.T) {
	in := carryFixture(VerdictApproved)

	result := CarryForward(in)

	require.Equal(t, []string{in.Rows[0].StableID}, result.Carried)
	assert.Empty(t, result.Losses)
	require.NotNil(t, in.Rows[0].CarriedFrom,
		"a carried row records the run its verdict came from, or nothing downstream can re-check it")
	assert.Equal(t, in.BaseRunID, *in.Rows[0].CarriedFrom)
}

// TestCarryForwardRefusesAnUnapprovedProposal is the regression for a
// proposal that was carried and then had nowhere left to go.
//
// A carried row is dropped from the queue and from the review gate, and the
// new run holds no verdict for it, so carrying a proposal nobody approved
// removed the row from every surface that could still act on it — the human's
// pending approval simply stopped being asked for. The row must re-queue, and
// the reason must say what happened.
func TestCarryForwardRefusesAnUnapprovedProposal(t *testing.T) {
	in := carryFixture(VerdictProposed)

	result := CarryForward(in)

	assert.Empty(t, result.Carried)
	require.Len(t, result.Losses, 1)
	assert.Equal(t, in.Rows[0].StableID, result.Losses[0].StableID)
	assert.Contains(t, result.Losses[0].Reason, "never approved")
	assert.Nil(t, in.Rows[0].CarriedFrom, "a row that did not carry is not marked as carried")
}

// TestCarryForwardNamesEveryLoss covers the reporting half of the feature.
// A row dropping back into review without a reason is what makes an
// incremental run feel arbitrary, so each class of refusal is checked for a
// reason that names the actual cause.
func TestCarryForwardNamesEveryLoss(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*CarryForwardInput)
		wantReason string
	}{
		{
			name: "a changed row cites its own evidence",
			mutate: func(in *CarryForwardInput) {
				in.Classes[in.Rows[0].StableID] = ClassChanged
			},
			wantReason: "classified changed",
		},
		{
			name: "a row the diff never saw is new to this run",
			mutate: func(in *CarryForwardInput) {
				delete(in.Classes, in.Rows[0].StableID)
			},
			wantReason: "classified new",
		},
		{
			name: "a profile change that touches the row names the key",
			mutate: func(in *CarryForwardInput) {
				next := normalizedDefault()
				next.Evidence.DepthByStage[in.Rows[0].AttentionStage] = EvidenceDepthNone
				in.NextProfile = next
			},
			wantReason: "evidence depth for stage",
		},
		{
			name: "a vocabulary that dropped the label says it was removed",
			mutate: func(in *CarryForwardInput) {
				in.NextPolicies[in.Rows[0].Type] = TypePolicy{
					Dispositions: map[string]CanonicalAction{
						"shelved": CanonicalAction("attention/parked"),
					},
				}
			},
			wantReason: `disposition "parked" was removed`,
		},
		{
			// Both runs froze the same vocabulary, so there is no delta to
			// report — but it is a vocabulary the verdict's own label was
			// never in. This is the run that predates a policy the campaign
			// has since replaced wholesale.
			name: "a verdict camp can no longer execute names the disposition",
			mutate: func(in *CarryForwardInput) {
				narrow := TypePolicy{
					Dispositions: map[string]CanonicalAction{
						"shelved": CanonicalAction("attention/parked"),
					},
				}
				in.BasePolicies[in.Rows[0].Type] = narrow
				in.NextPolicies[in.Rows[0].Type] = narrow
			},
			wantReason: "no longer maps to an action",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := carryFixture(VerdictApproved)
			tt.mutate(&in)

			result := CarryForward(in)

			assert.Empty(t, result.Carried)
			require.Len(t, result.Losses, 1)
			assert.Contains(t, result.Losses[0].Reason, tt.wantReason)
		})
	}
}

// TestCarryForwardIgnoresAnUnjudgedRow: a row nobody decided in the base run
// is not a loss, it is simply work that was never done. Reporting it as a lost
// carry would bury the rows that really did lose one.
func TestCarryForwardIgnoresAnUnjudgedRow(t *testing.T) {
	in := carryFixture(VerdictApproved)
	in.BaseVerdicts = map[string]RowVerdict{}

	result := CarryForward(in)

	assert.Empty(t, result.Carried)
	assert.Empty(t, result.Losses)
}

// TestCarryForwardComparesFrozenVocabularies is the regression for comparing
// the campaign's live type policy instead of the two runs' frozen ones.
//
// Once policies come off disk, "did the vocabulary move" is a question about
// what each run actually recorded. Reading the built-in for both answers no
// every time, which silently carries verdicts expressed in a label the
// campaign has since redefined.
func TestCarryForwardComparesFrozenVocabularies(t *testing.T) {
	in := carryFixture(VerdictApproved)
	rowType := in.Rows[0].Type
	in.BasePolicies[rowType] = TypePolicy{
		Dispositions: map[string]CanonicalAction{"parked": CanonicalAction("attention/parked")},
	}
	in.NextPolicies[rowType] = TypePolicy{
		Dispositions: map[string]CanonicalAction{"parked": CanonicalAction("dungeon/archived")},
	}

	result := CarryForward(in)

	assert.Empty(t, result.Carried,
		"a label that now means a different mutation cannot carry the old verdict")
	require.Len(t, result.Losses, 1)
	assert.Contains(t, result.Losses[0].Reason, "parked")
}

// TestCarryForwardLossesAreAlwaysAnArray keeps the JSON contract stable: a
// consumer indexing carry_losses must never have to tell absent from empty.
func TestCarryForwardLossesAreAlwaysAnArray(t *testing.T) {
	result := CarryForward(carryFixture(VerdictApproved))
	assert.NotNil(t, result.Losses)
}
