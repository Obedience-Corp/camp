package triage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// planRow builds a manifest row for the compiler tests.
func planRow(stableID, wfType, attention string) ManifestRow {
	return ManifestRow{
		StableID:       stableID,
		Key:            wfType + ":workflow/" + wfType + "/" + stableID,
		Type:           wfType,
		Title:          stableID,
		RelativePath:   "workflow/" + wfType + "/" + stableID,
		LifecycleStage: "none",
		AttentionStage: attention,
		Batch:          1,
	}
}

// approvedVerdict is a live approved verdict for one action.
func approvedVerdict(stableID, disposition string, action CanonicalAction) RowVerdict {
	return RowVerdict{
		StableID:        stableID,
		State:           VerdictApproved,
		Disposition:     disposition,
		CanonicalAction: action,
		At:              testAt,
		Events:          2,
	}
}

// compileOne compiles a single row and returns its entry.
func compileOne(t *testing.T, row ManifestRow, verdict RowVerdict) ApplyPlanEntry {
	t.Helper()
	plan, err := CompilePlan(CompileInput{
		RunID:    "run-20260810T140000Z",
		Rows:     []ManifestRow{row},
		Verdicts: map[string]RowVerdict{row.StableID: verdict},
		Now:      testAt,
	})
	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)
	return plan.Entries[0]
}

// TestCompilePlanMapsEveryCanonicalAction pins the argv for every action camp
// can execute. Each of these was verified against the real CLI: a mapping that
// compiles is not the same as a mapping that runs.
func TestCompilePlanMapsEveryCanonicalAction(t *testing.T) {
	tests := []struct {
		name     string
		action   CanonicalAction
		wantArgv []string
		wantKind CommandKind
		wantUndo []string
	}{
		{
			name:     "attention stage is positional, not a flag",
			action:   CanonicalAction("attention/parked"),
			wantArgv: []string{"camp", "workitem", "stage", "design-a", "parked"},
			wantKind: CommandKindAttention,
			wantUndo: []string{"camp", "workitem", "stage", "design-a", "active"},
		},
		{
			name:     "attention current",
			action:   CanonicalAction("attention/current"),
			wantArgv: []string{"camp", "workitem", "stage", "design-a", "current"},
			wantKind: CommandKindAttention,
			wantUndo: []string{"camp", "workitem", "stage", "design-a", "active"},
		},
		{
			name:     "rail promote to ready",
			action:   CanonicalAction("rail/ready"),
			wantArgv: []string{"camp", "workitem", "promote", "design-a", "--target", "ready"},
			wantKind: CommandKindRail,
			wantUndo: []string{"camp", "workitem", "demote", "design-a"},
		},
		{
			name:     "rail promote to active",
			action:   CanonicalAction("rail/active"),
			wantArgv: []string{"camp", "workitem", "promote", "design-a", "--target", "active"},
			wantKind: CommandKindRail,
			wantUndo: []string{"camp", "workitem", "demote", "design-a"},
		},
		{
			name:     "dungeon completed",
			action:   CanonicalAction("dungeon/completed"),
			wantArgv: []string{"camp", "workitem", "promote", "design-a", "--target", "completed"},
			wantKind: CommandKindDungeon,
			// No compile-time undo: the destination carries a dated bucket.
			wantUndo: nil,
		},
		{
			name:     "dungeon archived",
			action:   CanonicalAction("dungeon/archived"),
			wantArgv: []string{"camp", "workitem", "promote", "design-a", "--target", "archived"},
			wantKind: CommandKindDungeon,
			wantUndo: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := planRow("design-a", "design", "active")
			entry := compileOne(t, row, approvedVerdict("design-a", "d", tt.action))

			require.Len(t, entry.Commands, 1)
			assert.Equal(t, tt.wantArgv, entry.Commands[0].Argv)
			assert.Equal(t, tt.wantKind, entry.Commands[0].Kind)
			assert.Equal(t, tt.wantUndo, nilIfEmpty(entry.Undo))

			// Every entry carries the freshness precondition.
			assert.Contains(t, entry.Preconditions, Precondition{Kind: PreconditionRowFresh})
			assert.True(t, entry.Executable())
		})
	}
}

// nilIfEmpty normalizes the empty slice Normalize writes back to nil, so a
// table can express "no undo" as nil.
func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

// TestApplyCommandForRendersTheSameMapping is the anti-drift guard: the string
// a human is shown must be a rendering of the argv the executor records, not a
// second mapping. The two disagreeing is precisely how the review surface came
// to print `camp workitem attention`, which is not a command camp has.
func TestApplyCommandForRendersTheSameMapping(t *testing.T) {
	row := planRow("design-a", "design", "active")
	for _, action := range CanonicalActions() {
		got := ApplyCommandFor(row, CanonicalAction(action))
		argv := ApplyArgvFor(row, CanonicalAction(action))
		if len(argv) == 0 {
			assert.Empty(t, got, "action %q", action)
			continue
		}
		assert.Equal(t, strings.Join(argv, " "), got, "action %q", action)
	}
}

// TestApplyArgvUsesRealCommands guards the specific error this sequence found.
func TestApplyArgvUsesRealCommands(t *testing.T) {
	row := planRow("design-a", "design", "active")

	stage := ApplyArgvFor(row, CanonicalAction("attention/parked"))
	assert.Equal(t, "stage", stage[2],
		"the attention verb is `camp workitem stage`; `camp workitem attention` does not exist")
	assert.NotContains(t, stage, "--set",
		"stage takes its value positionally")

	for _, action := range CanonicalActions() {
		argv := ApplyArgvFor(row, CanonicalAction(action))
		for _, arg := range argv {
			assert.NotEqual(t, "flow", arg,
				"there is no `camp flow`; spec doc 04's undo example names a command camp does not have")
		}
	}
}

// TestCompilePlanOrdering pins the ordering rule: splits first, then moves,
// attention last, deterministic by stable id inside a group.
func TestCompilePlanOrdering(t *testing.T) {
	rows := []ManifestRow{
		planRow("z-attention", "design", "active"),
		planRow("a-dungeon", "design", "active"),
		planRow("m-split", "design", "active"),
		planRow("b-rail", "design", "active"),
		planRow("a-attention", "design", "active"),
	}
	verdicts := map[string]RowVerdict{
		"z-attention": approvedVerdict("z-attention", "parked", CanonicalAction("attention/parked")),
		"a-dungeon":   approvedVerdict("a-dungeon", "completed", CanonicalAction("dungeon/completed")),
		"m-split":     approvedVerdict("m-split", "consolidated", ActionSplit),
		"b-rail":      approvedVerdict("b-rail", "ready", CanonicalAction("rail/ready")),
		"a-attention": approvedVerdict("a-attention", "parked", CanonicalAction("attention/parked")),
	}

	plan, err := CompilePlan(CompileInput{
		RunID: "run-1", Rows: rows, Verdicts: verdicts,
		Successors:     map[string][]string{"m-split": {"part-b", "part-a"}},
		SplitAvailable: true,
		Now:            testAt,
	})
	require.NoError(t, err)

	got := make([]string, 0, len(plan.Entries))
	for _, entry := range plan.Entries {
		got = append(got, entry.StableID)
	}
	assert.Equal(t, []string{
		"m-split",             // splits first
		"a-dungeon", "b-rail", // then moves, sorted within the group
		"a-attention", "z-attention", // attention last, sorted
	}, got)
}

// TestCompilePlanIsDeterministic pins the property --dry-run depends on.
func TestCompilePlanIsDeterministic(t *testing.T) {
	rows := []ManifestRow{
		planRow("design-b", "design", "active"),
		planRow("design-a", "design", "parked"),
	}
	verdicts := map[string]RowVerdict{
		"design-a": approvedVerdict("design-a", "completed", CanonicalAction("dungeon/completed")),
		"design-b": approvedVerdict("design-b", "parked", CanonicalAction("attention/parked")),
	}
	in := CompileInput{RunID: "run-1", Rows: rows, Verdicts: verdicts, Now: testAt}

	first, err := CompilePlan(in)
	require.NoError(t, err)
	for range 5 {
		again, err := CompilePlan(in)
		require.NoError(t, err)
		assert.Equal(t, first, again)
	}
}

// TestCompilePlanSkipsUnapplicableVerdicts: only approved verdicts compile.
func TestCompilePlanSkipsUnapplicableVerdicts(t *testing.T) {
	tests := []struct {
		name    string
		verdict RowVerdict
	}{
		{name: "never judged", verdict: RowVerdict{}},
		{
			name:    "proposed but not approved",
			verdict: RowVerdict{State: VerdictProposed, CanonicalAction: CanonicalAction("attention/parked")},
		},
		{
			name:    "rejected",
			verdict: RowVerdict{State: VerdictRejected, CanonicalAction: CanonicalAction("attention/parked")},
		},
		{
			name:    "staled by a refresh",
			verdict: RowVerdict{State: VerdictStale, CanonicalAction: CanonicalAction("attention/parked")},
		},
		{
			name:    "approved but the action changes nothing",
			verdict: RowVerdict{State: VerdictApproved, CanonicalAction: ActionNone},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := CompilePlan(CompileInput{
				RunID:    "run-1",
				Rows:     []ManifestRow{planRow("design-a", "design", "active")},
				Verdicts: map[string]RowVerdict{"design-a": tt.verdict},
				Now:      testAt,
			})
			require.NoError(t, err)
			assert.Empty(t, plan.Entries)
		})
	}
}

// TestCompilePlanBlocksConsolidationUntilSplitLands covers the phase-ordering
// requirement: the row compiles, carries its successors, and does not run.
func TestCompilePlanBlocksConsolidationUntilSplitLands(t *testing.T) {
	row := planRow("design-umbrella", "design", "active")
	plan, err := CompilePlan(CompileInput{
		RunID: "run-1", Rows: []ManifestRow{row},
		Verdicts:   map[string]RowVerdict{"design-umbrella": approvedVerdict("design-umbrella", "consolidated", ActionSplit)},
		Successors: map[string][]string{"design-umbrella": {"part-b", "part-a"}},
		// Phase 004 has not landed the verb yet.
		SplitAvailable: false,
		Now:            testAt,
	})
	require.NoError(t, err, "a blocked row must not fail the whole plan")
	require.Len(t, plan.Entries, 1)

	entry := plan.Entries[0]
	assert.False(t, entry.Executable())
	assert.Contains(t, entry.Blocked, "requires camp workitem split")
	assert.Contains(t, entry.Blocked, "part-a")
	assert.Contains(t, entry.Blocked, "part-b")
	assert.Empty(t, entry.Commands, "there is nothing to run yet")

	// It still carries the D2 gate, so the executor cannot later run it
	// without checking the successors this verdict promised.
	assert.Contains(t, entry.Preconditions, Precondition{
		Kind: PreconditionSuccessorsExist, IDs: []string{"part-a", "part-b"},
	})

	assert.Len(t, plan.BlockedEntries(), 1)
	assert.Empty(t, plan.ExecutableEntries())
}

// TestCompilePlanConsolidationOnceSplitLands is the same row with the phase-004
// capability flipped.
func TestCompilePlanConsolidationOnceSplitLands(t *testing.T) {
	row := planRow("design-umbrella", "design", "active")
	plan, err := CompilePlan(CompileInput{
		RunID: "run-1", Rows: []ManifestRow{row},
		Verdicts:       map[string]RowVerdict{"design-umbrella": approvedVerdict("design-umbrella", "consolidated", ActionSplit)},
		Successors:     map[string][]string{"design-umbrella": {"part-b", "part-a"}},
		SplitAvailable: true,
		Now:            testAt,
	})
	require.NoError(t, err)
	require.Len(t, plan.Entries, 1)

	entry := plan.Entries[0]
	assert.True(t, entry.Executable())
	assert.Empty(t, entry.Blocked)
	require.Len(t, entry.Commands, 2)

	// Split first, then the parent's retirement — D2's ordering, in one entry.
	assert.Equal(t, CommandKindSplit, entry.Commands[0].Kind)
	assert.Equal(t,
		[]string{"camp", "workitem", "split", "design-umbrella", "--into", "part-a", "--into", "part-b"},
		entry.Commands[0].Argv,
		"successors are sorted so the command is reproducible")
	assert.Equal(t, CommandKindDungeon, entry.Commands[1].Kind)
	assert.Equal(t,
		[]string{"camp", "workitem", "promote", "design-umbrella", "--target", "completed"},
		entry.Commands[1].Argv)
}

// TestCompilePlanBlocksConsolidationWithoutSuccessors pins the review
// finding: an approved consolidation that declared no successors must compile
// blocked, never as an executable command carrying the display placeholder
// that apply would extract as a real successor name.
func TestCompilePlanBlocksConsolidationWithoutSuccessors(t *testing.T) {
	row := planRow("design-umbrella", "design", "active")
	plan, err := CompilePlan(CompileInput{
		RunID: "run-1", Rows: []ManifestRow{row},
		Verdicts:       map[string]RowVerdict{"design-umbrella": approvedVerdict("design-umbrella", "consolidated", ActionSplit)},
		Successors:     map[string][]string{},
		SplitAvailable: true,
		Now:            testAt,
	})
	require.NoError(t, err, "a blocked row must not fail the whole plan")
	require.Len(t, plan.Entries, 1)

	entry := plan.Entries[0]
	assert.False(t, entry.Executable())
	assert.Contains(t, entry.Blocked, "no successors")
	assert.Empty(t, entry.Commands, "nothing runnable may carry the placeholder")

	assert.Len(t, plan.BlockedEntries(), 1)
	assert.Empty(t, plan.ExecutableEntries())
}

// TestApplyPlanValidateRejectsSuccessorPlaceholder guards the stored-plan
// path: a stale or hand-edited plan document whose split argv still carries
// the placeholder must fail validation before apply can run it.
func TestApplyPlanValidateRejectsSuccessorPlaceholder(t *testing.T) {
	plan := &ApplyPlan{
		RunID:     "run-1",
		CreatedAt: testAt,
		Entries: []ApplyPlanEntry{{
			StableID:  "design-umbrella",
			VerdictAt: testAt,
			Commands: []PlanCommand{{
				Argv: []string{"camp", "workitem", "split", "design-umbrella", "--into", splitSuccessorPlaceholder},
				Kind: CommandKindSplit,
			}},
		}},
	}
	plan.Normalize()

	violations := plan.Validate()
	require.NotEmpty(t, violations, "placeholder argv must not validate")
	found := false
	for _, v := range violations {
		if strings.Contains(v.Message, "placeholder") {
			found = true
		}
	}
	assert.True(t, found, "violation should name the placeholder, got %v", violations)
}

// TestCompilePlanUndoForAClearedStage covers the row that had no attention
// stage: its undo has to clear rather than set an empty string.
func TestCompilePlanUndoForAClearedStage(t *testing.T) {
	row := planRow("design-a", "design", "")
	entry := compileOne(t, row, approvedVerdict("design-a", "parked", CanonicalAction("attention/parked")))
	assert.Equal(t,
		[]string{"camp", "workitem", "stage", "design-a", "clear"}, entry.Undo,
		"a row with no prior stage undoes to clear, which is what the command accepts")
}

// TestFT012TrialApprovedSetCompilesToWhatWasRunByHand is the sequence's Done
// When: the three rows approved in the 2026-08-03 trial and promoted by hand
// on 2026-08-04 must compile to exactly those promote commands.
func TestFT012TrialApprovedSetCompilesToWhatWasRunByHand(t *testing.T) {
	ids := []string{
		"design-camp-artifact-commit-updates-2026-07-25",
		"design-festival-hub-control-plane-2026-08-04",
		"design-workitem-priority-cli-2026-07-30",
	}

	rows := make([]ManifestRow, 0, len(ids))
	verdicts := make(map[string]RowVerdict, len(ids))
	for _, id := range ids {
		rows = append(rows, planRow(id, "design", "active"))
		verdicts[id] = approvedVerdict(id, "completed", CanonicalAction("dungeon/completed"))
	}

	plan, err := CompilePlan(CompileInput{
		RunID: "run-20260804T120000Z", Rows: rows, Verdicts: verdicts, Now: testAt,
	})
	require.NoError(t, err)
	require.Len(t, plan.Entries, 3)

	// Exactly the commands that were run by hand, in a reproducible order.
	var got []string
	for _, entry := range plan.Entries {
		require.Len(t, entry.Commands, 1)
		got = append(got, entry.Commands[0].String())
	}
	assert.Equal(t, []string{
		"camp workitem promote design-camp-artifact-commit-updates-2026-07-25 --target completed",
		"camp workitem promote design-festival-hub-control-plane-2026-08-04 --target completed",
		"camp workitem promote design-workitem-priority-cli-2026-07-30 --target completed",
	}, got)

	for _, entry := range plan.Entries {
		assert.True(t, entry.Executable())
		assert.Equal(t, CommandKindDungeon, entry.Commands[0].Kind)
		assert.Contains(t, entry.Preconditions, Precondition{Kind: PreconditionRowFresh})
	}
}

// TestCompilePlanIsPure guards the reuse rule.
func TestCompilePlanIsPure(t *testing.T) {
	rows := []ManifestRow{planRow("design-a", "design", "active")}
	before := rows[0]
	successors := map[string][]string{"design-a": {"part-b", "part-a"}}

	_, err := CompilePlan(CompileInput{
		RunID: "run-1", Rows: rows,
		Verdicts:       map[string]RowVerdict{"design-a": approvedVerdict("design-a", "c", ActionSplit)},
		Successors:     successors,
		SplitAvailable: true,
		Now:            testAt,
	})
	require.NoError(t, err)

	assert.Equal(t, before, rows[0], "the caller's rows must come back untouched")
	assert.Equal(t, []string{"part-b", "part-a"}, successors["design-a"],
		"the caller's successor list must not be sorted in place")
}
