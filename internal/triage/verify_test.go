package triage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

// appliedReceipt is a receipt for one executed command.
func appliedReceipt(stableID string, kind CommandKind, argv []string, undo string) Receipt {
	return Receipt{
		StableID: stableID, Kind: kind, Argv: argv, Undo: undo,
		StartedAt: testAt, FinishedAt: testAt, Result: ReceiptApplied,
	}
}

// TestExpectationFor derives what each action kind promises. Pure: the table
// is the whole contract.
func TestExpectationFor(t *testing.T) {
	row := planRow("design-a", "design", "active")

	tests := []struct {
		name    string
		receipt Receipt
		want    Expectation
	}{
		{
			name: "attention sets a stage and does not move the item",
			receipt: appliedReceipt("design-a", CommandKindAttention,
				[]string{"camp", "workitem", "stage", "design-a", "parked"}, ""),
			want: Expectation{
				StableID: "design-a", Kind: CommandKindAttention,
				Path: row.RelativePath, Stage: "parked",
			},
		},
		{
			name: "clearing a stage expects no stage, not the literal clear",
			receipt: appliedReceipt("design-a", CommandKindAttention,
				[]string{"camp", "workitem", "stage", "design-a", "clear"}, ""),
			want: Expectation{
				StableID: "design-a", Kind: CommandKindAttention, Path: row.RelativePath,
			},
		},
		{
			name: "a dungeon promote expects the item to be gone, at the landed path",
			receipt: appliedReceipt("design-a", CommandKindDungeon,
				[]string{"camp", "workitem", "promote", "design-a", "--target", "completed"},
				"camp move workflow/design/.dungeon/completed/2026-08-05/design-a workflow/design/design-a"),
			want: Expectation{
				StableID: "design-a", Kind: CommandKindDungeon, Gone: true,
				Path: "workflow/design/.dungeon/completed/2026-08-05/design-a",
			},
		},
		{
			name: "a rail promote expects the item at the landed path",
			receipt: appliedReceipt("design-a", CommandKindRail,
				[]string{"camp", "workitem", "promote", "design-a", "--target", "ready"},
				"camp move workflow/design/ready/design-a workflow/design/design-a"),
			want: Expectation{
				StableID: "design-a", Kind: CommandKindRail,
				Path: "workflow/design/ready/design-a",
			},
		},
		{
			name: "a split expects its successors",
			receipt: appliedReceipt("design-a", CommandKindSplit,
				[]string{"camp", "workitem", "split", "design-a", "--into", "part-a", "--into", "part-b"}, ""),
			want: Expectation{
				StableID: "design-a", Kind: CommandKindSplit, Path: row.RelativePath,
				Successors: []string{"part-a", "part-b"},
			},
		},
		{
			name: "a dungeon promote with no recorded undo falls back to the row's path",
			receipt: appliedReceipt("design-a", CommandKindDungeon,
				[]string{"camp", "workitem", "promote", "design-a", "--target", "completed"}, ""),
			want: Expectation{
				StableID: "design-a", Kind: CommandKindDungeon, Gone: true,
				Path: row.RelativePath,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExpectationFor(tt.receipt, row))
		})
	}
}

// discovered builds an index holding the given items.
func discovered(items ...DiscoveredItem) DiscoveryIndex {
	index := DiscoveryIndex{ByStableID: map[string]DiscoveredItem{}, FestivalStatus: map[string]string{}}
	for _, item := range items {
		index.ByStableID[item.StableID] = item
	}
	return index
}

// TestVerifyOne covers match and mismatch for every kind.
func TestVerifyOne(t *testing.T) {
	at := func(path, stage string) DiscoveredItem {
		return DiscoveredItem{StableID: "design-a", RelativePath: path, AttentionStage: stage}
	}

	tests := []struct {
		name        string
		expectation Expectation
		index       DiscoveryIndex
		want        VerificationResult
	}{
		{
			name:        "attention matches when the stage is what was set",
			expectation: Expectation{StableID: "design-a", Kind: CommandKindAttention, Path: "p", Stage: "parked"},
			index:       discovered(at("p", "parked")),
			want:        VerificationMatch,
		},
		{
			name:        "attention mismatches when someone moved it back",
			expectation: Expectation{StableID: "design-a", Kind: CommandKindAttention, Path: "p", Stage: "parked"},
			index:       discovered(at("p", "active")),
			want:        VerificationMismatch,
		},
		{
			name:        "a retired item matches by being undiscoverable",
			expectation: Expectation{StableID: "design-a", Kind: CommandKindDungeon, Gone: true, Path: "d"},
			index:       discovered(),
			want:        VerificationMatch,
		},
		{
			name:        "a retired item that is still there mismatches",
			expectation: Expectation{StableID: "design-a", Kind: CommandKindDungeon, Gone: true, Path: "d"},
			index:       discovered(at("p", "active")),
			want:        VerificationMismatch,
		},
		{
			name:        "a non-terminal row that vanished mismatches",
			expectation: Expectation{StableID: "design-a", Kind: CommandKindAttention, Path: "p", Stage: "parked"},
			index:       discovered(),
			want:        VerificationMismatch,
		},
		{
			name:        "a rail move matches at its landed path",
			expectation: Expectation{StableID: "design-a", Kind: CommandKindRail, Path: "ready/design-a"},
			index:       discovered(at("ready/design-a", "active")),
			want:        VerificationMatch,
		},
		{
			name:        "a rail move mismatches somewhere else",
			expectation: Expectation{StableID: "design-a", Kind: CommandKindRail, Path: "ready/design-a"},
			index:       discovered(at("workflow/design/design-a", "active")),
			want:        VerificationMismatch,
		},
		{
			name: "a split matches when every successor exists",
			expectation: Expectation{
				StableID: "design-a", Kind: CommandKindSplit, Path: "p",
				Successors: []string{"part-a"},
			},
			index: discovered(at("p", "active"),
				DiscoveredItem{StableID: "part-a", RelativePath: "workflow/design/part-a"}),
			want: VerificationMatch,
		},
		{
			name: "a split mismatches when a successor is missing",
			expectation: Expectation{
				StableID: "design-a", Kind: CommandKindSplit, Path: "p",
				Successors: []string{"part-a", "part-b"},
			},
			index: discovered(at("p", "active"),
				DiscoveredItem{StableID: "part-a", RelativePath: "workflow/design/part-a"}),
			want: VerificationMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := verifyOne(tt.expectation, tt.index, "")
			assert.Equal(t, tt.want, got.Result)
		})
	}
}

// TestVerifyOneExplanationSuppressesFailure: an accounted-for mismatch is
// still reported as a mismatch, it just stops failing the run.
func TestVerifyOneExplanationSuppressesFailure(t *testing.T) {
	expectation := Expectation{StableID: "design-a", Kind: CommandKindAttention, Path: "p", Stage: "parked"}
	got := verifyOne(expectation, discovered(DiscoveredItem{
		StableID: "design-a", RelativePath: "p", AttentionStage: "active",
	}), "un-parked on purpose during the release")

	assert.Equal(t, VerificationMismatch, got.Result,
		"the difference is still recorded honestly")
	assert.NotEmpty(t, got.Explanation)

	report := &VerificationReport{RunID: "r", CheckedAt: testAt, Rows: []VerificationRow{got}}
	report.Normalize()
	assert.True(t, report.Clean(), "an explained mismatch does not fail the run")
	assert.Empty(t, report.Unexplained())
}

// TestLatestAppliedPerRow: a retry supersedes the failure before it.
func TestLatestAppliedPerRow(t *testing.T) {
	receipts := []Receipt{
		{StableID: "a", Result: ReceiptFailed, Error: "boom"},
		{StableID: "a", Result: ReceiptApplied, Argv: []string{"second"}},
		{StableID: "b", Result: ReceiptApplied, Argv: []string{"only"}},
		{StableID: "c", Result: ReceiptFailed, Error: "never applied"},
	}

	got := latestAppliedPerRow(receipts)
	require.Len(t, got, 2, "a row that only ever failed is not verified as applied")
	assert.Equal(t, "a", got[0].StableID)
	assert.Equal(t, []string{"second"}, got[0].Argv, "the retry, not the failure")
	assert.Equal(t, "b", got[1].StableID)
}

// TestVerifyThroughTheStore covers the clean and tampered paths end to end,
// including the phase transition and the rendered document.
func TestVerifyThroughTheStore(t *testing.T) {
	ctx := context.Background()
	store, run := applyStore(t)
	plan := planFor(t, run, map[string]CanonicalAction{
		hubID: CanonicalAction("attention/parked"),
	})
	applyWith(t, store, run, plan, &fakeMover{}, allReady(plan))

	parked := []workitem.WorkItem{{
		StableID: hubID, WorkflowType: "design",
		Key:            "design:workflow/design/festival-hub-control-plane",
		RelativePath:   "workflow/design/festival-hub-control-plane",
		LifecycleStage: "active", AttentionStage: "parked",
	}}

	clean, err := store.Verify(ctx, VerifyInput{RunID: run.ID, Items: parked, Now: testAt})
	require.NoError(t, err)
	assert.True(t, clean.Clean())
	assert.Equal(t, 1, clean.Totals.Checked)
	assert.Equal(t, 1, clean.Totals.Matched)

	dataPath, docPath, err := store.WriteVerification(ctx, clean)
	require.NoError(t, err)
	assert.Contains(t, dataPath, "verification.json")
	assert.Contains(t, docPath, "VERIFICATION.md")

	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, PhaseVerified, reopened.State.Phase,
		"a clean verification is what moves a run to verified")

	// Tamper: someone put it back.
	tampered := parked
	tampered[0].AttentionStage = "active"
	dirty, err := store.Verify(ctx, VerifyInput{RunID: run.ID, Items: tampered, Now: testAt})
	require.NoError(t, err)
	assert.False(t, dirty.Clean())
	require.Len(t, dirty.Unexplained(), 1)
	assert.Equal(t, hubID, dirty.Unexplained()[0].StableID)
	assert.Equal(t, "parked", dirty.Unexplained()[0].ExpectedStage)
	assert.Equal(t, "active", dirty.Unexplained()[0].DiscoveredStage)
}

// TestVerifyDoesNotVerifyADirtyRun: the phase only moves when the campaign
// actually matches, or "verified" would mean "someone ran verify".
func TestVerifyDoesNotVerifyADirtyRun(t *testing.T) {
	ctx := context.Background()
	store, run := applyStore(t)
	plan := planFor(t, run, map[string]CanonicalAction{
		hubID: CanonicalAction("attention/parked"),
	})
	applyWith(t, store, run, plan, &fakeMover{}, allReady(plan))

	// Discovery finds nothing, so the row cannot be where it should be.
	report, err := store.Verify(ctx, VerifyInput{RunID: run.ID, Now: testAt})
	require.NoError(t, err)
	require.False(t, report.Clean())

	_, _, err = store.WriteVerification(ctx, report)
	require.NoError(t, err)

	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, PhaseApplying, reopened.State.Phase)
}

// TestVerifyReadsReceiptsNotThePlan is the sequence's quality standard: a plan
// entry with no receipt is not verified, because nothing ran for it.
func TestVerifyReadsReceiptsNotThePlan(t *testing.T) {
	ctx := context.Background()
	store, run := applyStore(t)

	// A compiled plan exists for both rows, but nothing was applied.
	planFor(t, run, map[string]CanonicalAction{
		hubID:    CanonicalAction("attention/parked"),
		legacyID: CanonicalAction("attention/parked"),
	})

	report, err := store.Verify(ctx, VerifyInput{RunID: run.ID, Now: testAt})
	require.NoError(t, err)
	assert.Equal(t, 0, report.Totals.Checked,
		"verification describes what happened, and nothing did")
	assert.True(t, report.Clean())
}

// TestVerifyRequiresARunID covers the input guard.
func TestVerifyRequiresARunID(t *testing.T) {
	store, _ := applyStore(t)
	_, err := store.Verify(context.Background(), VerifyInput{Now: testAt})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run_id")
}

// TestVerifyHonorsCancellation is the context rule.
func TestVerifyHonorsCancellation(t *testing.T) {
	store, run := applyStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.Verify(ctx, VerifyInput{RunID: run.ID, Now: testAt})
	assert.ErrorIs(t, err, context.Canceled)
}

// TestRenderVerificationIsByteStable pins the renderer discipline the other
// documents follow: the report is the source of truth, the document a view.
func TestRenderVerificationIsByteStable(t *testing.T) {
	report := &VerificationReport{
		RunID: "run-20260810T140000Z", CheckedAt: testAt,
		Rows: []VerificationRow{
			{StableID: "design-a", ExpectedPath: "p", ExpectedStage: "parked",
				DiscoveredPath: "p", DiscoveredStage: "parked", Result: VerificationMatch},
			{StableID: "design-b", ExpectedPath: "q", ExpectedStage: "parked",
				DiscoveredPath: "q", DiscoveredStage: "active", Result: VerificationMismatch},
		},
	}
	report.Normalize()

	first := RenderVerification(report)
	for range 3 {
		assert.Equal(t, first, RenderVerification(report))
	}

	body := string(first)
	assert.Contains(t, body, "Do not edit")
	assert.Contains(t, body, "camp triage verify")
	assert.Contains(t, body, "design-a")
	assert.Contains(t, body, "1 row(s) are not where their verdict said")
	assert.NotContains(t, body, time.Now().Format("15:04:05"),
		"the renderer must read no clock of its own")
}

// TestVerifyCountsAnApprovedRowThatNeverApplied is the phase 006 acceptance
// blocker, at the level where it can be constructed exactly.
//
// Verification used to iterate only the rows that produced an applied receipt.
// A row whose apply failed produced none, so it was checked by nothing and the
// report came back clean by omission — "N checked, N matched, 0 mismatched" on
// a run that had not executed one of its approved decisions, which then closed
// as `verified`.
//
// The row is now counted as `unapplied`, named, and treated as unexplained, so
// the run stays out of `verified` until the decision actually runs.
func TestVerifyCountsAnApprovedRowThatNeverApplied(t *testing.T) {
	ctx := context.Background()
	store, run := applyStore(t)

	// Two rows approved; only one applies. The other's verdict stays live, so
	// it is a decision camp still owes.
	plan := planFor(t, run, map[string]CanonicalAction{
		hubID: CanonicalAction("attention/parked"),
	})
	applyWith(t, store, run, plan, &fakeMover{}, allReady(plan))

	parked := []workitem.WorkItem{{
		StableID: hubID, WorkflowType: "design",
		Key:            "design:workflow/design/festival-hub-control-plane",
		RelativePath:   "workflow/design/festival-hub-control-plane",
		LifecycleStage: "active", AttentionStage: "parked",
	}}

	// A second approved verdict that apply never reached.
	other := run.Manifest.Rows[1].StableID
	require.NotEqual(t, hubID, other)
	require.NoError(t, store.AppendDecision(ctx, run.ID, DecisionEvent{
		Event: DecisionApproved, StableID: other,
		Disposition: "parked", CanonicalAction: CanonicalAction("attention/parked"),
		Actor: "tester", At: testAt,
	}))

	report, err := store.Verify(ctx, VerifyInput{RunID: run.ID, Items: parked, Now: testAt})
	require.NoError(t, err)

	assert.Equal(t, 1, report.Totals.Unapplied,
		"the approved row that never ran is counted, not skipped")
	assert.False(t, report.Clean(),
		"a run owing an unexecuted decision is not clean")

	var named bool
	for _, row := range report.Rows {
		if row.StableID == other && row.Result == VerificationUnapplied {
			named = true
		}
	}
	assert.True(t, named, "and is named, so an operator knows which decision did not happen")

	_, _, err = store.WriteVerification(ctx, report)
	require.NoError(t, err)
	reopened, err := store.OpenRun(ctx, run.ID)
	require.NoError(t, err)
	assert.NotEqual(t, PhaseVerified, reopened.State.Phase,
		"and the run does not reach verified while it owes one")
}
