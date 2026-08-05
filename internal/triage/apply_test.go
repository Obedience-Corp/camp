package triage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// fakeMover records what it was asked to do and can be made to fail.
type fakeMover struct {
	calls []moverCall
	// failOn makes the mover fail for a stable id.
	failOn map[string]bool
}

type moverCall struct {
	verb     string
	stableID string
	arg      string
	args     []string
}

func (m *fakeMover) Stage(_ context.Context, stableID, stage string) (MoveOutcome, error) {
	m.calls = append(m.calls, moverCall{verb: "stage", stableID: stableID, arg: stage})
	if m.failOn[stableID] {
		return MoveOutcome{}, camperrors.New("stage refused")
	}
	return MoveOutcome{
		Undo:   "camp workitem stage " + stableID + " clear",
		Commit: "abc" + stableID,
	}, nil
}

func (m *fakeMover) MoveIdea(_ context.Context, stableID, status, _ string) (MoveOutcome, error) {
	m.calls = append(m.calls, moverCall{verb: "idea", stableID: stableID, arg: status})
	if m.failOn[stableID] {
		return MoveOutcome{}, camperrors.New("idea move refused")
	}
	return MoveOutcome{Undo: "camp idea move " + stableID + " inbox"}, nil
}

func (m *fakeMover) Promote(_ context.Context, stableID, target string) (MoveOutcome, error) {
	m.calls = append(m.calls, moverCall{verb: "promote", stableID: stableID, arg: target})
	if m.failOn[stableID] {
		return MoveOutcome{}, camperrors.New("promote refused: destination exists")
	}
	return MoveOutcome{
		Undo:   "camp move workflow/design/.dungeon/" + target + "/2026-08-10/" + stableID + " workflow/design/" + stableID,
		Commit: "def" + stableID,
	}, nil
}

func (m *fakeMover) Split(_ context.Context, stableID string, successors []string) (MoveOutcome, error) {
	m.calls = append(m.calls, moverCall{verb: "split", stableID: stableID, args: successors})
	if m.failOn[stableID] {
		return MoveOutcome{}, camperrors.New("split refused")
	}
	return MoveOutcome{Undo: "camp workitem split --undo " + stableID}, nil
}

// applyStore is a run advanced far enough that apply is a legal next step.
func applyStore(t *testing.T) (*Store, *Run) {
	t.Helper()
	ctx := context.Background()
	store := NewStore(t.TempDir(), func() time.Time { return testAt })

	manifest := newManifestForStore()
	manifest.Rows[1].CarriedFrom = nil
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	// Judge every row, then walk the real phase path. created -> applying is
	// not an edge, and the review gate refuses a run with unjudged rows. Both
	// refusals are correct, so the fixture satisfies them rather than
	// bypassing them: an apply test that started from an impossible run state
	// would prove nothing about apply.
	for _, row := range run.Manifest.Rows {
		record := validEvidence()
		record.StableID = row.StableID
		record.Anchors = nil
		_, err = store.WriteEvidence(ctx, run.ID, record)
		require.NoError(t, err)
		_, err = store.Propose(ctx, ProposeInput{
			RunID: run.ID, StableID: row.StableID, Disposition: "parked",
			Rationale: rationale("because"), Actor: "tester", Now: testAt,
		})
		require.NoError(t, err)
	}
	for _, phase := range []Phase{PhaseSnapshotted, PhaseJudging, PhaseReviewing} {
		_, err = store.SetPhase(ctx, run.ID, phase, "test fixture")
		require.NoError(t, err)
	}
	return store, run
}

// planFor compiles a plan over the store's two rows with the given actions.
func planFor(t *testing.T, run *Run, actions map[string]CanonicalAction) *ApplyPlan {
	t.Helper()
	verdicts := make(map[string]RowVerdict, len(actions))
	for id, action := range actions {
		verdicts[id] = approvedVerdict(id, "d", action)
	}
	plan, err := CompilePlan(CompileInput{
		RunID: run.ID, Rows: run.Manifest.Rows, Verdicts: verdicts, Now: testAt,
	})
	require.NoError(t, err)
	return plan
}

// allReady marks every entry in a plan ready to apply.
func allReady(plan *ApplyPlan) map[string]ApplyReadiness {
	out := make(map[string]ApplyReadiness, len(plan.Entries))
	for _, entry := range plan.Entries {
		out[entry.StableID] = ApplyReady
	}
	return out
}

func applyWith(t *testing.T, store *Store, run *Run, plan *ApplyPlan, mover Mover,
	readiness map[string]ApplyReadiness) *ApplyResult {
	t.Helper()
	result, err := store.Apply(context.Background(), ApplyInput{
		RunID: run.ID, Plan: plan, Mover: mover, Actor: "tester",
		Now: func() time.Time { return testAt }, Readiness: readiness,
	})
	require.NoError(t, err)
	return result
}

// TestApplyExecutesEveryRowAndWritesReceipts is the happy path.
func TestApplyExecutesEveryRowAndWritesReceipts(t *testing.T) {
	ctx := context.Background()
	store, run := applyStore(t)
	plan := planFor(t, run, map[string]CanonicalAction{
		hubID:    CanonicalAction("attention/parked"),
		legacyID: CanonicalAction("dungeon/completed"),
	})
	mover := &fakeMover{}

	result := applyWith(t, store, run, plan, mover, allReady(plan))

	assert.ElementsMatch(t, []string{hubID, legacyID}, result.Applied)
	assert.Empty(t, result.Skipped)
	assert.False(t, result.Halted)

	// The dungeon move ran before the attention change, per the plan order.
	require.Len(t, mover.calls, 2)
	assert.Equal(t, "promote", mover.calls[0].verb)
	assert.Equal(t, "completed", mover.calls[0].arg)
	assert.Equal(t, "stage", mover.calls[1].verb)
	assert.Equal(t, "parked", mover.calls[1].arg)

	receipts, err := store.Receipts(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, receipts, 2)
	for _, receipt := range receipts {
		assert.Equal(t, ReceiptApplied, receipt.Result)
		assert.NotEmpty(t, receipt.Undo, "the executor records the real undo")
		assert.NotEmpty(t, receipt.Commit)
		assert.NotEmpty(t, receipt.Argv, "the argv is the audit record")
	}

	// The phase moved before the first action, so a kill is legible.
	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, PhaseApplying, reopened.State.Phase)
}

// TestApplyStopsAtAFailureAndLeavesTheRestPending is the safe default: a
// failed move does not silently continue into rows that may depend on it.
func TestApplyStopsAtAFailureAndLeavesTheRestPending(t *testing.T) {
	ctx := context.Background()
	store, run := applyStore(t)
	plan := planFor(t, run, map[string]CanonicalAction{
		hubID:    CanonicalAction("attention/parked"),
		legacyID: CanonicalAction("dungeon/completed"),
	})
	// The dungeon row runs first, so failing it must stop the attention row.
	mover := &fakeMover{failOn: map[string]bool{legacyID: true}}

	result := applyWith(t, store, run, plan, mover, allReady(plan))

	assert.True(t, result.Halted)
	assert.Equal(t, legacyID, result.Failed)
	assert.Empty(t, result.Applied)
	assert.Len(t, mover.calls, 1, "the pass stopped rather than continuing")

	receipts, err := store.Receipts(ctx, run.ID)
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.Equal(t, ReceiptFailed, receipts[0].Result)
	assert.Contains(t, receipts[0].Error, "destination exists")
	assert.Empty(t, receipts[0].Undo, "a failed action has nothing to undo")
	assert.Empty(t, receipts[0].Commit)
}

// TestApplyResumesFromTheFirstUnappliedRow is the re-run property.
func TestApplyResumesFromTheFirstUnappliedRow(t *testing.T) {
	store, run := applyStore(t)
	plan := planFor(t, run, map[string]CanonicalAction{
		hubID:    CanonicalAction("attention/parked"),
		legacyID: CanonicalAction("dungeon/completed"),
	})

	// First pass fails on the dungeon row.
	first := applyWith(t, store, run, plan, &fakeMover{failOn: map[string]bool{legacyID: true}}, allReady(plan))
	require.True(t, first.Halted)

	// Second pass: the failure is retried, and it succeeds this time.
	mover := &fakeMover{}
	second := applyWith(t, store, run, plan, mover, allReady(plan))

	assert.False(t, second.Halted)
	assert.ElementsMatch(t, []string{hubID, legacyID}, second.Applied)
	assert.Len(t, mover.calls, 2,
		"a row whose last receipt failed is retried, not treated as done")

	// Third pass: everything is applied, so nothing runs again.
	idle := &fakeMover{}
	third := applyWith(t, store, run, plan, idle, allReady(plan))
	assert.Empty(t, idle.calls, "an applied row never re-executes")
	assert.Empty(t, third.Applied)
	require.Len(t, third.Skipped, 2)
	for _, skipped := range third.Skipped {
		assert.Contains(t, skipped.Reason, "already applied")
	}
}

// TestApplyRefusesRowsTheRefreshBlocked covers the staleness gate.
func TestApplyRefusesRowsTheRefreshBlocked(t *testing.T) {
	store, run := applyStore(t)
	plan := planFor(t, run, map[string]CanonicalAction{
		hubID:    CanonicalAction("attention/parked"),
		legacyID: CanonicalAction("dungeon/completed"),
	})

	readiness := map[string]ApplyReadiness{
		hubID:    ApplyReady,
		legacyID: ApplyBlockedStale,
	}
	mover := &fakeMover{}
	result := applyWith(t, store, run, plan, mover, readiness)

	assert.Equal(t, []string{hubID}, result.Applied)
	require.Len(t, result.Skipped, 1)
	assert.Equal(t, legacyID, result.Skipped[0].StableID)
	assert.Equal(t, string(ApplyBlockedStale), result.Skipped[0].Reason)

	for _, call := range mover.calls {
		assert.NotEqual(t, legacyID, call.stableID, "a blocked row never reaches the mover")
	}
}

// TestApplyRefusesARowWithNoRefreshResult is the difference between "stale"
// and "never checked". Assuming fresh here would defeat the refresh entirely.
func TestApplyRefusesARowWithNoRefreshResult(t *testing.T) {
	store, run := applyStore(t)
	plan := planFor(t, run, map[string]CanonicalAction{
		hubID: CanonicalAction("attention/parked"),
	})

	mover := &fakeMover{}
	result := applyWith(t, store, run, plan, mover, map[string]ApplyReadiness{})

	assert.Empty(t, result.Applied)
	assert.Empty(t, mover.calls)
	require.Len(t, result.Skipped, 1)
	assert.Contains(t, result.Skipped[0].Reason, "no refresh result")
	assert.Contains(t, result.Skipped[0].Reason, "camp triage refresh")
}

// TestApplySkipsBlockedConsolidations: a row waiting on phase 004's split
// verb is reported with its reason rather than attempted.
func TestApplySkipsBlockedConsolidations(t *testing.T) {
	store, run := applyStore(t)
	plan, err := CompilePlan(CompileInput{
		RunID: run.ID, Rows: run.Manifest.Rows,
		Verdicts: map[string]RowVerdict{
			hubID: approvedVerdict(hubID, "consolidated", ActionSplit),
		},
		Successors:     map[string][]string{hubID: {"part-a"}},
		SplitAvailable: false,
		Now:            testAt,
	})
	require.NoError(t, err)

	mover := &fakeMover{}
	result := applyWith(t, store, run, plan, mover, allReady(plan))

	assert.Empty(t, mover.calls)
	require.Len(t, result.Skipped, 1)
	assert.Contains(t, result.Skipped[0].Reason, "requires camp workitem split")
	assert.Contains(t, result.Skipped[0].Reason, "part-a")
}

// TestApplyRunsSplitBeforeTheParentMove covers a consolidation's two commands
// in one entry: the successors exist before the parent is retired (D2).
func TestApplyRunsSplitBeforeTheParentMove(t *testing.T) {
	store, run := applyStore(t)
	plan, err := CompilePlan(CompileInput{
		RunID: run.ID, Rows: run.Manifest.Rows,
		Verdicts: map[string]RowVerdict{
			hubID: approvedVerdict(hubID, "consolidated", ActionSplit),
		},
		Successors:     map[string][]string{hubID: {"part-b", "part-a"}},
		SplitAvailable: true,
		Now:            testAt,
	})
	require.NoError(t, err)

	mover := &fakeMover{}
	result := applyWith(t, store, run, plan, mover, allReady(plan))

	require.Len(t, mover.calls, 2)
	assert.Equal(t, "split", mover.calls[0].verb)
	assert.Equal(t, []string{"part-a", "part-b"}, mover.calls[0].args)
	assert.Equal(t, "promote", mover.calls[1].verb)
	assert.Equal(t, "completed", mover.calls[1].arg)
	assert.Equal(t, []string{hubID}, result.Applied)
}

// TestApplyHaltsBetweenCommandsOfOneEntry: if the split fails, the parent is
// not retired. Retiring a parent whose successors do not exist is the exact
// data loss D2 exists to prevent.
func TestApplyHaltsBetweenCommandsOfOneEntry(t *testing.T) {
	store, run := applyStore(t)
	plan, err := CompilePlan(CompileInput{
		RunID: run.ID, Rows: run.Manifest.Rows,
		Verdicts: map[string]RowVerdict{
			hubID: approvedVerdict(hubID, "consolidated", ActionSplit),
		},
		Successors:     map[string][]string{hubID: {"part-a"}},
		SplitAvailable: true,
		Now:            testAt,
	})
	require.NoError(t, err)

	mover := &fakeMover{failOn: map[string]bool{hubID: true}}
	result := applyWith(t, store, run, plan, mover, allReady(plan))

	assert.True(t, result.Halted)
	require.Len(t, mover.calls, 1)
	assert.Equal(t, "split", mover.calls[0].verb,
		"the parent promote must not run after its split failed")
}

// TestApplyValidatesItsInputs covers the required-argument errors.
func TestApplyValidatesItsInputs(t *testing.T) {
	store, run := applyStore(t)
	plan := planFor(t, run, map[string]CanonicalAction{hubID: CanonicalAction("attention/parked")})

	tests := []struct {
		name  string
		in    ApplyInput
		field string
	}{
		{name: "run id", in: ApplyInput{Plan: plan, Mover: &fakeMover{}, Actor: "t"}, field: "run_id"},
		{name: "plan", in: ApplyInput{RunID: run.ID, Mover: &fakeMover{}, Actor: "t"}, field: "plan"},
		{name: "mover", in: ApplyInput{RunID: run.ID, Plan: plan, Actor: "t"}, field: "mover"},
		{name: "actor", in: ApplyInput{RunID: run.ID, Plan: plan, Mover: &fakeMover{}}, field: "actor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.Apply(context.Background(), tt.in)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.field)
		})
	}
}

// TestApplyHonorsCancellation is the context rule.
func TestApplyHonorsCancellation(t *testing.T) {
	store, run := applyStore(t)
	plan := planFor(t, run, map[string]CanonicalAction{hubID: CanonicalAction("attention/parked")})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.Apply(ctx, ApplyInput{
		RunID: run.ID, Plan: plan, Mover: &fakeMover{}, Actor: "tester",
		Readiness: allReady(plan),
	})
	assert.ErrorIs(t, err, context.Canceled)
}

// TestApplyReadinessFromDiff wires the refresh's classes to apply's gate.
func TestApplyReadinessFromDiff(t *testing.T) {
	diff := Diff{Rows: []RowDiff{
		{StableID: "fresh-row", Class: ClassFresh},
		{StableID: "stale-row", Class: ClassChanged},
		{StableID: "unchecked-row", Class: ClassFresh, UncheckedAnchors: 1},
	}}
	actions := map[string]CanonicalAction{
		"fresh-row":     CanonicalAction("attention/parked"),
		"stale-row":     CanonicalAction("attention/parked"),
		"unchecked-row": CanonicalAction("dungeon/completed"),
	}

	got := ApplyReadinessFromDiff(diff, actions, false)
	assert.Equal(t, ApplyReady, got["fresh-row"])
	assert.Equal(t, ApplyBlockedStale, got["stale-row"])
	assert.Equal(t, ApplyBlockedUnchecked, got["unchecked-row"],
		"a terminal action over an unverified anchor is blocked")

	forced := ApplyReadinessFromDiff(diff, actions, true)
	assert.Equal(t, ApplyReady, forced["unchecked-row"], "--force covers missing information")
	assert.Equal(t, ApplyBlockedStale, forced["stale-row"], "--force never covers a stale verdict")
}
