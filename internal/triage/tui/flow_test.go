package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/crawl"
	"github.com/Obedience-Corp/camp/internal/triage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testAt = time.Date(2026, 8, 10, 14, 0, 0, 0, time.UTC)

// scriptedPrompt answers the flow's prompts from a fixed script, which is the
// test seam crawl.Prompt exists for. It records what it was shown so a test
// can assert on the option sets the flow offered, not only on what it chose.
type scriptedPrompt struct {
	actions      []crawl.Action
	destinations []string
	actionIndex  int
	destIndex    int

	SeenActions [][]crawl.Option
	SeenTitles  []string
}

func (p *scriptedPrompt) SelectAction(_ context.Context, item crawl.Item, options []crawl.Option) (crawl.Action, error) {
	p.SeenActions = append(p.SeenActions, options)
	p.SeenTitles = append(p.SeenTitles, item.Title)
	if p.actionIndex >= len(p.actions) {
		return actionQuit, nil
	}
	action := p.actions[p.actionIndex]
	p.actionIndex++
	return action, nil
}

func (p *scriptedPrompt) SelectDestination(_ context.Context, _ crawl.Item, _ []crawl.Option) (crawl.Option, error) {
	if p.destIndex >= len(p.destinations) {
		return crawl.Option{}, nil
	}
	target := p.destinations[p.destIndex]
	p.destIndex++
	return crawl.Option{Action: crawl.ActionMove, Target: target}, nil
}

func (p *scriptedPrompt) Reason(_ context.Context, _ crawl.Item, _ crawl.Option) (string, error) {
	return "", nil
}

// offeredLabels flattens the labels of one prompt the flow raised.
func offeredLabels(options []crawl.Option) string {
	labels := make([]string, 0, len(options))
	for _, option := range options {
		labels = append(labels, option.Label)
	}
	return strings.Join(labels, " | ")
}

// flowFixture builds a run with a parked lane and a terminal lane, each
// holding live proposals.
func flowFixture(t *testing.T) (*triage.Store, *triage.Run) {
	t.Helper()
	ctx := context.Background()
	store := triage.NewStore(t.TempDir(), func() time.Time { return testAt })

	manifest := &triage.Manifest{
		SchemaVersion: triage.SchemaVersion,
		Mode:          triage.RunModeFull,
		Profile: triage.ManifestProfile{
			Name: triage.ProfileNameDefault, Resolved: triage.DefaultProfile(),
		},
		CreatedAt: testAt,
	}
	for _, spec := range []struct{ id, title string }{
		{"design-park-a", "Park A"},
		{"design-park-b", "Park B"},
		{"design-done-a", "Done A"},
	} {
		manifest.Rows = append(manifest.Rows, triage.ManifestRow{
			StableID: spec.id, Key: "design:" + spec.id, Type: "design",
			Title: spec.title, RelativePath: "workflow/design/" + spec.id,
			LifecycleStage: "active", Batch: 1,
			Policy: triage.RowPolicy{
				Evidence: triage.EvidenceDepthDeep, RoutingTier: triage.RoutingTierDefault,
			},
		})
	}
	run, err := store.CreateRun(ctx, manifest)
	require.NoError(t, err)

	dispositions := map[string]string{
		"design-park-a": "parked",
		"design-park-b": "parked",
		"design-done-a": "completed",
	}
	for _, row := range run.Manifest.Rows {
		record := &triage.EvidenceRecord{
			SchemaVersion: triage.SchemaVersion,
			StableID:      row.StableID,
			OriginalGoal:  "ship " + row.StableID,
			Delivered:     []string{"the core"},
			Missing:       []string{"the docs"},
			OpenDecisions: []string{"who owns the follow-up"},
			Confidence:    triage.ConfidenceHigh,
			ProducedBy: triage.ProducedBy{
				Role: triage.EvidenceRoleEvidence, Runtime: "fixture", At: testAt,
			},
		}
		_, err := store.WriteEvidence(ctx, run.ID, record)
		require.NoError(t, err)
		_, err = store.Propose(ctx, triage.ProposeInput{
			RunID: run.ID, StableID: row.StableID,
			Disposition: dispositions[row.StableID],
			Rationale: &triage.Rationale{
				SchemaVersion: triage.SchemaVersion,
				Summary:       "recorded for the flow test",
				Confidence:    triage.ConfidenceHigh,
			},
			Actor: "tester", Now: testAt,
		})
		require.NoError(t, err)
	}
	return store, run
}

func newFlow(store *triage.Store, run *triage.Run, prompt crawl.Prompt, out *bytes.Buffer) *Flow {
	return &Flow{
		Store: store, RunID: run.ID, Prompt: prompt, Out: out,
		Actor: "lancekrogers", Now: func() time.Time { return testAt },
	}
}

// --- the flow records through the same path as the CLI -----------------

// TestApproveKeystrokeRecordsThroughTheStore is the invariant that keeps the
// TUI honest: it records nothing itself, so a verdict entered here is the same
// event `camp triage approve` writes.
func TestApproveKeystrokeRecordsThroughTheStore(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)
	var out bytes.Buffer

	prompt := &scriptedPrompt{actions: []crawl.Action{
		actionNextLane, // summary: review the first lane
		actionNextLane, // lane: open the first row
		actionApprove,  // row: approve
		actionBack,     // lane: back to summary
		actionQuit,     // summary: quit
	}}
	result, err := newFlow(store, run, prompt, &out).Run(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Recorded)

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	approved := 0
	for _, verdict := range verdicts {
		if verdict.State == triage.VerdictApproved {
			approved++
			assert.Equal(t, "lancekrogers", verdict.Actor)
		}
	}
	assert.Equal(t, 1, approved, "exactly the row the flow approved")
}

// TestQuitSavesProgress: the append-only stream is the bookmark, so quitting
// mid-lane loses nothing.
func TestQuitSavesProgress(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)
	var out bytes.Buffer

	prompt := &scriptedPrompt{actions: []crawl.Action{
		actionNextLane, actionNextLane, actionApprove, actionQuit,
	}}
	result, err := newFlow(store, run, prompt, &out).Run(ctx)

	require.NoError(t, err)
	assert.True(t, result.Quit)
	assert.Equal(t, 1, result.Recorded)

	events, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	var approvals int
	for _, event := range events {
		if event.Event == triage.DecisionApproved {
			approvals++
		}
	}
	assert.Equal(t, 1, approvals, "the verdict survived the quit")
}

// TestReopenResumesAtTheFirstUndecidedRow: a second session must not re-ask
// about rows already decided.
func TestReopenResumesAtTheFirstUndecidedRow(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)

	first := &scriptedPrompt{actions: []crawl.Action{
		actionNextLane, actionNextLane, actionApprove, actionQuit,
	}}
	_, err := newFlow(store, run, first, &bytes.Buffer{}).Run(ctx)
	require.NoError(t, err)

	// Reopen: the lane view should now offer the *other* parked row.
	second := &scriptedPrompt{actions: []crawl.Action{actionNextLane, actionQuit}}
	_, err = newFlow(store, run, second, &bytes.Buffer{}).Run(ctx)
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(second.SeenActions), 2)
	laneOptions := offeredLabels(second.SeenActions[1])
	assert.Contains(t, laneOptions, "design-park-b",
		"the second session opens the row the first left undecided")
	assert.NotContains(t, laneOptions, "design-park-a")
}

// TestEscRecordsNothing: backing out is not a verdict.
func TestEscRecordsNothing(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)

	prompt := &scriptedPrompt{actions: []crawl.Action{
		actionNextLane, actionNextLane, actionBack, actionBack, actionQuit,
	}}
	result, err := newFlow(store, run, prompt, &bytes.Buffer{}).Run(ctx)

	require.NoError(t, err)
	assert.Equal(t, 0, result.Recorded)
	events, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	for _, event := range events {
		assert.NotEqual(t, triage.DecisionApproved, event.Event)
	}
}

// TestAmendUsesTheTypeVocabulary drives [m] through the destination picker.
func TestAmendUsesTheTypeVocabulary(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)

	prompt := &scriptedPrompt{
		actions:      []crawl.Action{actionNextLane, actionNextLane, actionAmend, actionQuit},
		destinations: []string{"archived"},
	}
	result, err := newFlow(store, run, prompt, &bytes.Buffer{}).Run(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Recorded)

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, "archived", verdicts["design-park-a"].Disposition)
	assert.Equal(t, triage.CanonicalAction("dungeon/archived"),
		verdicts["design-park-a"].CanonicalAction)
}

// --- the safety rules --------------------------------------------------

// TestTerminalLaneOffersNoBulkApproval: offering a key that then refuses would
// be worse than not offering it.
func TestTerminalLaneOffersNoBulkApproval(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)

	// Pick the terminal lane explicitly, then look at what it offered.
	prompt := &scriptedPrompt{
		actions:      []crawl.Action{actionPickLane, actionBack, actionQuit},
		destinations: []string{"close-as-delivered"},
	}
	_, err := newFlow(store, run, prompt, &bytes.Buffer{}).Run(ctx)
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(prompt.SeenActions), 2)
	laneOptions := offeredLabels(prompt.SeenActions[1])
	assert.NotContains(t, laneOptions, "Approve this whole lane",
		"a terminal lane never offers bulk approval")
}

// TestNonTerminalLaneOffersBulkApprovalUnderTheProfile.
func TestNonTerminalLaneOffersBulkApprovalUnderTheProfile(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)

	prompt := &scriptedPrompt{actions: []crawl.Action{actionNextLane, actionBack, actionQuit}}
	_, err := newFlow(store, run, prompt, &bytes.Buffer{}).Run(ctx)
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(prompt.SeenActions), 2)
	assert.Contains(t, offeredLabels(prompt.SeenActions[1]), "Approve this whole lane")
}

// TestLaneApprovalConfirmsAndListsWhatItCovers: an unconfirmed bulk approval
// records nothing.
func TestLaneApprovalConfirmsAndListsWhatItCovers(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)
	var out bytes.Buffer

	// Answer the confirmation with "no".
	prompt := &scriptedPrompt{actions: []crawl.Action{
		actionNextLane, actionApproveAll, actionBack, actionBack, actionQuit,
	}}
	result, err := newFlow(store, run, prompt, &out).Run(ctx)

	require.NoError(t, err)
	assert.Equal(t, 0, result.Recorded, "declining the confirmation records nothing")
	assert.Contains(t, out.String(), "design-park-a", "the confirmation lists what it covers")
	assert.Contains(t, out.String(), "design-park-b")
}

// TestLaneApprovalRecordsWhenConfirmed.
func TestLaneApprovalRecordsWhenConfirmed(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)

	prompt := &scriptedPrompt{actions: []crawl.Action{
		actionNextLane, actionApproveAll, actionApprove, actionBack, actionQuit,
	}}
	result, err := newFlow(store, run, prompt, &bytes.Buffer{}).Run(ctx)

	require.NoError(t, err)
	assert.Equal(t, 2, result.Recorded, "both parked rows")

	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, triage.VerdictApproved, verdicts["design-park-a"].State)
	assert.Equal(t, triage.VerdictApproved, verdicts["design-park-b"].State)
	assert.Equal(t, triage.VerdictProposed, verdicts["design-done-a"].State,
		"the terminal row is untouched by a lane approval")
}

// TestConfirmationIsNotATwoOptionBooleanSelect is the campaign trap made a
// test: huh's viewport math clips a two-option Select whose cursor sits on the
// last option, so the confirmation is an action prompt with labelled choices.
func TestConfirmationIsNotATwoOptionBooleanSelect(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)

	prompt := &scriptedPrompt{actions: []crawl.Action{
		actionNextLane, actionApproveAll, actionBack, actionBack, actionQuit,
	}}
	_, err := newFlow(store, run, prompt, &bytes.Buffer{}).Run(ctx)
	require.NoError(t, err)

	var confirmation []crawl.Option
	for i, title := range prompt.SeenTitles {
		if strings.HasPrefix(title, "Approve these") {
			confirmation = prompt.SeenActions[i]
		}
	}
	require.NotNil(t, confirmation, "the confirmation was raised")
	for _, option := range confirmation {
		assert.NotEmpty(t, option.Label, "each choice is labelled, not a bare boolean")
	}
	assert.Contains(t, offeredLabels(confirmation), "No - go back",
		"the declining choice is first, so the cursor never parks on the last option")
}

// TestOrderPutsTerminalLanesLast so the reader builds context before
// irreversible calls.
func TestOrderPutsTerminalLanesLast(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)

	prompt := &scriptedPrompt{actions: []crawl.Action{actionQuit}}
	_, err := newFlow(store, run, prompt, &bytes.Buffer{}).Run(ctx)
	require.NoError(t, err)

	require.NotEmpty(t, prompt.SeenActions)
	assert.Contains(t, offeredLabels(prompt.SeenActions[0]), "Review next lane - Park for later",
		"the non-terminal lane is offered first")
}

// TestSkipAdvancesToTheNextRow is the bug a pty run surfaced: skipping records
// nothing by design, so without remembering the skip the lane kept offering
// the same row as "next" and [s] was an infinite loop rather than
// "decide later".
func TestSkipAdvancesToTheNextRow(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)

	prompt := &scriptedPrompt{actions: []crawl.Action{
		actionNextLane, // summary -> parked lane
		actionNextLane, // open the first row
		actionSkip,     // defer it
		actionNextLane, // open the next row
		actionApprove,  // decide that one
		actionBack, actionQuit,
	}}
	result, err := newFlow(store, run, prompt, &bytes.Buffer{}).Run(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Skipped)
	assert.Equal(t, 1, result.Recorded)

	// The approved row must be the second one, which is only reachable if the
	// skip advanced past the first.
	verdicts, err := store.Verdicts(ctx, run.ID)
	require.NoError(t, err)
	assert.Equal(t, triage.VerdictApproved, verdicts["design-park-b"].State,
		"the skip advanced to the next row")
	assert.Equal(t, triage.VerdictProposed, verdicts["design-park-a"].State,
		"the skipped row is untouched, not decided")
}

// TestSkipRecordsNothing: deferring is not a verdict.
func TestSkipRecordsNothing(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)
	before, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)

	prompt := &scriptedPrompt{actions: []crawl.Action{
		actionNextLane, actionNextLane, actionSkip, actionBack, actionQuit,
	}}
	_, err = newFlow(store, run, prompt, &bytes.Buffer{}).Run(ctx)
	require.NoError(t, err)

	after, err := store.Decisions(ctx, run.ID)
	require.NoError(t, err)
	assert.Len(t, after, len(before), "a skip appends no event")
}

// TestLeavingTheLaneClearsSkips so "decide later" is actually reachable later.
func TestLeavingTheLaneClearsSkips(t *testing.T) {
	ctx := context.Background()
	store, run := flowFixture(t)

	prompt := &scriptedPrompt{actions: []crawl.Action{
		actionNextLane, actionNextLane, actionSkip, // skip the first row
		actionBack,     // back to summary, ending the lane visit
		actionNextLane, // re-enter the lane
		actionQuit,
	}}
	_, err := newFlow(store, run, prompt, &bytes.Buffer{}).Run(ctx)
	require.NoError(t, err)

	// The final lane prompt should offer the skipped row again.
	last := prompt.SeenActions[len(prompt.SeenActions)-1]
	assert.Contains(t, offeredLabels(last), "design-park-a",
		"re-entering the lane offers the deferred row again")
}
