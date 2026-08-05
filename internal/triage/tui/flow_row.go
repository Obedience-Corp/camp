package tui

import (
	"context"
	"io"

	"github.com/Obedience-Corp/camp/internal/crawl"
	"github.com/Obedience-Corp/camp/internal/triage"
)

// rowStep renders screen 3 and records the verdict its keystroke chose.
func (f *Flow) rowStep(ctx context.Context, lane triage.Lane, row triage.LaneRow, result *Result, skipped map[string]bool) (bool, error) {
	run, _, _, err := f.load(ctx)
	if err != nil {
		return false, err
	}
	evidence, err := f.Store.Evidence(ctx, f.RunID, row.Row.StableID)
	if err != nil {
		return false, err
	}

	position := 1
	for i, candidate := range lane.Rows {
		if candidate.Row.StableID == row.Row.StableID {
			position = i + 1
		}
	}
	f.write(Screen3(CardInput{
		Row:        row,
		Position:   position,
		Total:      len(lane.Rows),
		Lane:       triage.LaneSelectorName(lane.Title),
		Evidence:   evidence,
		Successors: SuccessorsFor(run, row),
		Compact:    row.Row.Policy.Evidence == triage.EvidenceDepthMetadata,
	}))

	options := []crawl.Option{
		{Label: "[y] approve", Action: actionApprove},
		{Label: "[n] reject", Action: actionReject},
		{Label: "[m] amend disposition", Action: actionAmend},
		{Label: "[o] open the evidence record", Action: actionOpen},
		{Label: "[s] skip - decide later", Action: actionSkip},
		{Label: "[esc] back to lane", Action: actionBack},
		{Label: "[q] quit - progress is saved", Action: actionQuit},
	}
	item := crawl.Item{
		ID:          row.Row.StableID,
		Title:       row.Row.Title,
		Description: string(row.Verdict.Disposition) + " → " + string(row.Verdict.CanonicalAction),
	}
	action, err := f.Prompt.SelectAction(ctx, item, options)
	if err != nil {
		return false, err
	}

	switch action {
	case actionApprove:
		return false, f.record(ctx, row, triage.ApproveInput{
			RunID:    f.RunID,
			Selector: triage.Selector{StableIDs: []string{row.Row.StableID}},
			Actor:    f.Actor, Now: f.Now(),
		}, result)
	case actionReject:
		return false, f.record(ctx, row, triage.ApproveInput{
			RunID:    f.RunID,
			Selector: triage.Selector{StableIDs: []string{row.Row.StableID}},
			Reject:   true, Actor: f.Actor, Now: f.Now(),
		}, result)
	case actionAmend:
		return false, f.amend(ctx, row, result)
	case actionOpen:
		f.writeEvidence(evidence)
		return false, nil
	case actionSkip:
		// Deferring, not deciding: nothing is recorded, and the lane stops
		// offering this row for the rest of the visit.
		result.Skipped++
		skipped[row.Row.StableID] = true
		return false, nil
	case actionBack:
		// esc records nothing, by contract.
		return false, nil
	default:
		result.Quit = true
		return true, nil
	}
}

// amend runs the shared destination picker over the row's type vocabulary.
func (f *Flow) amend(ctx context.Context, row triage.LaneRow, result *Result) error {
	policy := triage.TypePolicyFor(row.Row.Type)
	labels := policy.Labels()
	options := make([]crawl.Option, 0, len(labels))
	for _, label := range labels {
		action := policy.Dispositions[label]
		options = append(options, crawl.Option{
			Label:  label + " → " + string(action),
			Action: crawl.ActionMove,
			Target: label,
		})
	}

	choice, err := f.Prompt.SelectDestination(ctx, crawl.Item{
		ID:    row.Row.StableID,
		Title: "Amend " + row.Row.StableID,
	}, options)
	if err != nil || choice.Target == "" {
		return err
	}

	return f.record(ctx, row, triage.ApproveInput{
		RunID:    f.RunID,
		Selector: triage.Selector{StableIDs: []string{row.Row.StableID}},
		Amend:    choice.Target,
		Actor:    f.Actor, Now: f.Now(),
	}, result)
}

// record writes a verdict through the store and re-renders the documents.
func (f *Flow) record(ctx context.Context, row triage.LaneRow, in triage.ApproveInput, result *Result) error {
	approval, err := f.Store.Approve(ctx, in)
	if err != nil {
		return err
	}
	for _, recorded := range approval.Recorded {
		if recorded.Unchanged {
			f.write(recorded.StableID + ": already stood, nothing recorded\n")
			continue
		}
		result.Recorded++
		f.write(recorded.StableID + " → " + recorded.Disposition +
			" (" + recorded.Event + ")\n")
		if recorded.ApplyCommand != "" && recorded.CanonicalAction.Terminal() {
			f.write("  apply will run: " + recorded.ApplyCommand + "\n")
		}
	}
	return f.rerender(ctx)
}

// rerender keeps the documents in step with the verdicts.
func (f *Flow) rerender(ctx context.Context) error {
	_, err := f.Store.RenderDocuments(ctx, f.RunID)
	return err
}

// confirm asks a yes/no question.
//
// It uses the action prompt with two explicit options rather than a boolean
// Select, per the campaign trap list: huh's viewport math clips a two-option
// Select whose cursor sits on the last option.
func (f *Flow) confirm(ctx context.Context, question string) (bool, error) {
	action, err := f.Prompt.SelectAction(ctx, crawl.Item{ID: "confirm", Title: question},
		[]crawl.Option{
			{Label: "No - go back", Action: actionBack},
			{Label: "Yes - approve them", Action: actionApprove},
		})
	if err != nil {
		return false, err
	}
	return action == actionApprove, nil
}

// writeEvidence prints the full record for [o].
func (f *Flow) writeEvidence(record *triage.EvidenceRecord) {
	if record == nil {
		f.write("No evidence record for this row.\n")
		return
	}
	body, err := triage.MarshalTemplate(record)
	if err != nil {
		f.write("Could not render the evidence record.\n")
		return
	}
	f.write(string(body))
}

// load reads the run, its status, and its lanes.
func (f *Flow) load(ctx context.Context) (*triage.Run, *triage.Status, []triage.Lane, error) {
	run, err := f.Store.OpenRun(ctx, f.RunID)
	if err != nil {
		return nil, nil, nil, err
	}
	verdicts, err := f.Store.Verdicts(ctx, f.RunID)
	if err != nil {
		return nil, nil, nil, err
	}
	in, err := triage.LoadRenderInput(ctx, f.Store, f.RunID)
	if err != nil {
		return nil, nil, nil, err
	}
	status := triage.StatusFrom(run, verdicts)
	return run, status, triage.BuildLanes(run, verdicts, in.Rationales), nil
}

func (f *Flow) write(s string) {
	if f.Out != nil {
		_, _ = io.WriteString(f.Out, s)
	}
}
