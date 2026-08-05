package triage

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// approveStore returns a store whose run holds a proposal on every row.
func approveStore(t *testing.T) (*Store, *Run) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store := NewStore(root, func() time.Time { return testAt })

	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	dispositions := []string{"parked", "completed"}
	for i, row := range run.Manifest.Rows {
		record := validEvidence()
		record.StableID = row.StableID
		_, err := store.WriteEvidence(ctx, run.ID, record)
		require.NoError(t, err)
		_, err = store.Propose(ctx, ProposeInput{
			RunID: run.ID, StableID: row.StableID,
			Disposition: dispositions[i%len(dispositions)],
			Rationale:   rationale("because"), Actor: "tester", Now: testAt,
		})
		require.NoError(t, err)
	}
	return store, run
}

func approve(t *testing.T, store *Store, run *Run, in ApproveInput) *ApproveResult {
	t.Helper()
	if in.RunID == "" {
		in.RunID = run.ID
	}
	if in.Actor == "" {
		in.Actor = "lancekrogers"
	}
	if in.Now.IsZero() {
		in.Now = testAt
	}
	result, err := store.Approve(context.Background(), in)
	require.NoError(t, err)
	return result
}

// --- error cases first -------------------------------------------------

// TestApproveRequiresAnActor: an unattributed verdict cannot be questioned
// later, which defeats recorded judgment.
func TestApproveRequiresAnActor(t *testing.T) {
	store, run := approveStore(t)

	_, err := store.Approve(context.Background(), ApproveInput{
		RunID: run.ID, Selector: Selector{Lane: "park-for-later"}, Now: testAt,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, camperrors.ErrInvalidInput)
}

// TestApproveRefusesAmendWithReject: an amendment is an approval of a
// different disposition, so the two are contradictory instructions.
func TestApproveRefusesAmendWithReject(t *testing.T) {
	store, run := approveStore(t)

	_, err := store.Approve(context.Background(), ApproveInput{
		RunID: run.ID, Selector: Selector{Lane: "park-for-later"},
		Amend: "next", Reject: true, Actor: "tester", Now: testAt,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, camperrors.ErrInvalidInput)
	assert.Contains(t, err.Error(), "amendment is an approval")
}

// TestApproveOnAnUnproposedRowRecordsNothingButReportsWhy.
//
// The store reports facts and the command decides the exit code: naming a real
// row that holds no proposal is not "no rows matched", it is "there was
// nothing here to approve, and here is why". The command turns an empty
// Recorded into exit 2 with that reason attached.
func TestApproveOnAnUnproposedRowRecordsNothingButReportsWhy(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)
	target := run.Manifest.Rows[0].StableID

	result, err := store.Approve(ctx, ApproveInput{
		RunID: run.ID, Selector: Selector{StableIDs: []string{target}},
		Actor: "tester", Now: testAt,
	})

	require.NoError(t, err)
	assert.Empty(t, result.Recorded, "nothing to approve means nothing recorded")
	assert.Equal(t, []string{target}, result.SkippedNoProposal,
		"and the row is named so the operator knows which one and why")
}

// TestApproveOnANonexistentRowIsAnError: an id that names nothing is a typo,
// not an empty result.
func TestApproveOnANonexistentRowIsAnError(t *testing.T) {
	store, run := approveStore(t)

	_, err := store.Approve(context.Background(), ApproveInput{
		RunID: run.ID, Selector: Selector{StableIDs: []string{"no-such-row"}},
		Actor: "tester", Now: testAt,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-row")
}

// TestApproveOnAnAllTerminalBulkSelectorRecordsNothing: the safety rule can
// leave a bulk approval with nothing to do, and that has to be visible rather
// than look like success.
func TestApproveOnAnAllTerminalBulkSelectorRecordsNothing(t *testing.T) {
	ctx := context.Background()
	store, run := approveStore(t)

	result, err := store.Approve(ctx, ApproveInput{
		RunID: run.ID, Selector: Selector{Lane: "close-as-delivered"},
		Actor: "tester", Now: testAt,
	})

	require.NoError(t, err)
	assert.Empty(t, result.Recorded)
	assert.NotEmpty(t, result.SkippedTerminal,
		"the rows it refused to cover are named for an individual approval")
}

// TestAmendRevalidatesAgainstTheRowVocabulary: an amendment is a new
// disposition, so it is checked rather than assumed compatible.
func TestAmendRevalidatesAgainstTheRowVocabulary(t *testing.T) {
	store, run := approveStore(t)

	_, err := store.Approve(context.Background(), ApproveInput{
		RunID:    run.ID,
		Selector: Selector{StableIDs: []string{run.Manifest.Rows[0].StableID}},
		Amend:    "promote", // an intent label, not a design one
		Actor:    "tester", Now: testAt,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, camperrors.ErrInvalidInput)
	assert.Contains(t, err.Error(), "completed", "the refusal lists the row's real vocabulary")
}

// --- recording ---------------------------------------------------------

// TestApproveRecordsTheVerdictAndItsAction.
func TestApproveRecordsTheVerdictAndItsAction(t *testing.T) {
	ctx := context.Background()
	store, run := approveStore(t)
	target := run.Manifest.Rows[0].StableID

	result := approve(t, store, run, ApproveInput{Selector: Selector{StableIDs: []string{target}}})

	require.Len(t, result.Recorded, 1)
	assert.Equal(t, target, result.Recorded[0].StableID)
	assert.Equal(t, string(DecisionApproved), result.Recorded[0].Event)
	assert.False(t, result.Recorded[0].Unchanged)

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, VerdictApproved, verdicts[target].State)
	assert.Equal(t, "lancekrogers", verdicts[target].Actor)
}

// TestApproveEchoesTheApplyCommandForTerminalRows: approving a terminal action
// should not be consent to a label whose meaning the operator has to remember.
func TestApproveEchoesTheApplyCommandForTerminalRows(t *testing.T) {
	store, run := approveStore(t)

	// Row 1 was proposed "completed" by the fixture.
	terminal := run.Manifest.Rows[1].StableID
	result := approve(t, store, run, ApproveInput{Selector: Selector{StableIDs: []string{terminal}}})

	require.Len(t, result.Recorded, 1)
	assert.True(t, result.Recorded[0].CanonicalAction.Terminal())
	assert.Contains(t, result.Recorded[0].ApplyCommand, "camp workitem promote")
	assert.Contains(t, result.Recorded[0].ApplyCommand, "--target completed")
}

// TestApplyCommandCoversEveryAction is a drift guard: a verdict whose action
// renders no command would be approved with nothing shown for it.
func TestApplyCommandCoversEveryAction(t *testing.T) {
	row := ManifestRow{StableID: "row-1"}

	for _, action := range CanonicalActions() {
		t.Run(action, func(t *testing.T) {
			command := ApplyCommandFor(row, CanonicalAction(action))
			if action == string(ActionNone) {
				assert.Empty(t, command, "a no-op action runs nothing")
				return
			}
			assert.NotEmpty(t, command, "every real action renders a command")
			assert.Contains(t, command, "row-1")
			assert.True(t, strings.HasPrefix(command, "camp workitem "),
				"the echo has to be a command the operator could run: %s", command)
		})
	}
}

// TestAmendRecordsTheNewDispositionAndAction.
func TestAmendRecordsTheNewDispositionAndAction(t *testing.T) {
	ctx := context.Background()
	store, run := approveStore(t)
	target := run.Manifest.Rows[0].StableID

	result := approve(t, store, run, ApproveInput{
		Selector: Selector{StableIDs: []string{target}}, Amend: "archived",
	})

	require.Len(t, result.Recorded, 1)
	assert.Equal(t, string(DecisionAmended), result.Recorded[0].Event)
	assert.Equal(t, "archived", result.Recorded[0].Disposition)
	assert.Equal(t, CanonicalAction("dungeon/archived"), result.Recorded[0].CanonicalAction)

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, VerdictApproved, verdicts[target].State, "an amendment approves")
	assert.Equal(t, "archived", verdicts[target].Disposition)
}

// TestRejectReturnsTheRowToNeedingAProposal.
func TestRejectReturnsTheRowToNeedingAProposal(t *testing.T) {
	ctx := context.Background()
	store, run := approveStore(t)
	target := run.Manifest.Rows[0].StableID

	result := approve(t, store, run, ApproveInput{
		Selector: Selector{StableIDs: []string{target}},
		Reject:   true, Note: "not yet",
	})

	require.Len(t, result.Recorded, 1)
	assert.Equal(t, string(DecisionRejected), result.Recorded[0].Event)

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, VerdictRejected, verdicts[target].State)
	assert.Equal(t, "not yet", verdicts[target].Note)

	gaps, err := store.ReadyForReview(ctx, run.ID)
	require.NoError(t, err)
	var found bool
	for _, gap := range gaps {
		if gap.StableID == target {
			found = true
			assert.Equal(t, "proposal", gap.Missing)
		}
	}
	assert.True(t, found, "a rejected row needs a new proposal")
}

// --- idempotence -------------------------------------------------------

// TestDoubleApproveIsANoOpNotADuplicate: re-running a lane after fixing one
// row must not double-write the rest. The stream is an argument, not a log of
// keystrokes.
func TestDoubleApproveIsANoOpNotADuplicate(t *testing.T) {
	ctx := context.Background()
	store, run := approveStore(t)
	target := run.Manifest.Rows[0].StableID

	first := approve(t, store, run, ApproveInput{Selector: Selector{StableIDs: []string{target}}})
	require.False(t, first.Recorded[0].Unchanged)
	afterFirst, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)

	second := approve(t, store, run, ApproveInput{Selector: Selector{StableIDs: []string{target}}})

	require.Len(t, second.Recorded, 1)
	assert.True(t, second.Recorded[0].Unchanged, "reported as unchanged, not recorded again")

	afterSecond, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	assert.Len(t, afterSecond, len(afterFirst), "no second event was written")
}

// TestReApprovingADifferentDispositionIsNotANoOp: only an identical verdict is
// unchanged.
func TestReApprovingADifferentDispositionIsNotANoOp(t *testing.T) {
	store, run := approveStore(t)
	target := run.Manifest.Rows[0].StableID
	approve(t, store, run, ApproveInput{Selector: Selector{StableIDs: []string{target}}})

	amended := approve(t, store, run, ApproveInput{
		Selector: Selector{StableIDs: []string{target}}, Amend: "archived",
	})

	assert.False(t, amended.Recorded[0].Unchanged)
}

// --- partial approval, the trial's scenario ----------------------------

// TestPartialApprovalAcrossTwoSittings replays what hand-editing broke in the
// field trial: approve a few rows, come back later, approve a few more, and
// the fold has to stay coherent with the rest still pending.
func TestPartialApprovalAcrossTwoSittings(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	// A run wide enough to approve a subset of.
	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	extra := manifest.Rows[0]
	for i, id := range []string{"row-c", "row-d", "row-e"} {
		clone := extra
		clone.StableID = id
		clone.Key = "design:" + id
		clone.Ref = ""
		clone.RelativePath = "workflow/design/" + id
		clone.Batch = 1 + i%2
		manifest.Rows = append(manifest.Rows, clone)
	}
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	for _, row := range run.Manifest.Rows {
		record := validEvidence()
		record.StableID = row.StableID
		_, err := store.WriteEvidence(ctx, run.ID, record)
		require.NoError(t, err)
		_, err = store.Propose(ctx, ProposeInput{
			RunID: run.ID, StableID: row.StableID, Disposition: "parked",
			Rationale: rationale("revisit"), Actor: "tester", Now: testAt,
		})
		require.NoError(t, err)
	}
	total := len(run.Manifest.Rows)

	// First sitting: approve two rows.
	firstTwo := []string{run.Manifest.Rows[0].StableID, run.Manifest.Rows[1].StableID}
	sitting1 := approve(t, store, run, ApproveInput{Selector: Selector{StableIDs: firstTwo}})
	require.Len(t, sitting1.Recorded, 2)

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	approved, proposed := countStates(verdicts)
	assert.Equal(t, 2, approved)
	assert.Equal(t, total-2, proposed, "the rest are untouched, not implicitly approved")

	// Second sitting: approve one more, and re-approve one from before.
	sitting2 := approve(t, store, run, ApproveInput{
		Selector: Selector{StableIDs: []string{"row-c", firstTwo[0]}},
	})
	require.Len(t, sitting2.Recorded, 2)
	unchanged := 0
	for _, verdict := range sitting2.Recorded {
		if verdict.Unchanged {
			unchanged++
		}
	}
	assert.Equal(t, 1, unchanged, "the row approved in the first sitting is unchanged")

	verdicts, err = store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	approved, proposed = countStates(verdicts)
	assert.Equal(t, 3, approved, "three of the run's rows are approved")
	assert.Equal(t, total-3, proposed)

	// The rendered document has to agree with the fold.
	in, err := LoadRenderInput(ctx, store, run.ID)
	require.NoError(t, err)
	review := string(RenderReview(in))
	assert.Contains(t, review, "| Workitem | Verdict | Disposition | Decided by | Decided at |")
	for _, id := range []string{firstTwo[0], firstTwo[1], "row-c"} {
		assert.Contains(t, review, "| `"+id+"` | approved |",
			"%s appears in the approval record", id)
	}
	assert.Contains(t, review, "| **Total** | **"+strconv.Itoa(total)+"** |")
}

// countStates tallies approved and still-proposed rows.
func countStates(verdicts map[string]RowVerdict) (approved, proposed int) {
	for _, verdict := range verdicts {
		switch verdict.State {
		case VerdictApproved:
			approved++
		case VerdictProposed:
			proposed++
		}
	}
	return approved, proposed
}

// --- actor -------------------------------------------------------------

// TestResolveActorFallsBackRatherThanBlocking: losing the attribution is bad,
// losing the judgment because git was unconfigured would be worse.
func TestResolveActorFallsBackRatherThanBlocking(t *testing.T) {
	actor := ResolveActor(context.Background())

	assert.NotEmpty(t, actor, "an actor is always resolved")
}

// TestBulkAmendValidatesEverythingBeforeRecordingAnything: a bulk amend that
// failed partway would leave earlier rows written while still returning an
// error, so the operator could not tell from the failure whether anything
// landed. Pre-flighting makes "it errored" mean "nothing happened".
func TestBulkAmendValidatesEverythingBeforeRecordingAnything(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)

	// Two rows of different types sharing a lane, so one amend label can be
	// valid for the first and invalid for the second.
	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	manifest.Rows[1].Type = "intent"
	manifest.Rows[1].LifecycleStage = "inbox"
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	for i, row := range run.Manifest.Rows {
		record := validEvidence()
		record.StableID = row.StableID
		_, err := store.WriteEvidence(ctx, run.ID, record)
		require.NoError(t, err)

		disposition := "parked"
		if i == 1 {
			disposition = "park" // the intent vocabulary's label
		}
		_, err = store.Propose(ctx, ProposeInput{
			RunID: run.ID, StableID: row.StableID, Disposition: disposition,
			Rationale: rationale("later"), Actor: "tester", Now: testAt,
		})
		require.NoError(t, err)
	}

	before, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)

	// "archived" is a design label; the intent row has "drop" instead, so the
	// amend is valid for one row and invalid for the other.
	_, err = store.Approve(ctx, ApproveInput{
		RunID: run.ID,
		Selector: Selector{StableIDs: []string{
			run.Manifest.Rows[0].StableID, run.Manifest.Rows[1].StableID,
		}},
		Amend: "archived", Actor: "tester", Now: testAt,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, camperrors.ErrInvalidInput)

	after, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	assert.Len(t, after, len(before),
		"a refused bulk amend records nothing at all, not a partial prefix")
}
