package triage

import (
	"context"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ApproveInput is one approval, amendment, or rejection.
type ApproveInput struct {
	RunID    string
	Selector Selector
	// Amend replaces the proposed disposition. Empty approves what was
	// proposed.
	Amend string
	// Reject records a refusal instead of an approval.
	Reject bool
	Note   string
	Actor  string
	Now    time.Time
}

// RecordedVerdict is one verdict this call wrote.
type RecordedVerdict struct {
	StableID        string          `json:"stable_id"`
	Event           string          `json:"event"`
	Disposition     string          `json:"disposition"`
	CanonicalAction CanonicalAction `json:"canonical_action"`
	// ApplyCommand is what apply will run for this row, echoed so an operator
	// approving a terminal action sees exactly what it does.
	ApplyCommand string `json:"apply_command,omitempty"`
	// Unchanged marks a verdict that already stood, recorded as a no-op
	// rather than as a duplicate event.
	Unchanged bool `json:"unchanged"`
}

// ApproveResult reports everything the call did and deliberately did not do.
type ApproveResult struct {
	RunID    string            `json:"run_id"`
	Recorded []RecordedVerdict `json:"recorded"`
	// SkippedTerminal names rows a bulk selector refused to cover.
	SkippedTerminal []string `json:"skipped_terminal"`
	// SkippedNoProposal names rows with nothing to approve.
	SkippedNoProposal []string `json:"skipped_no_proposal"`
}

// Approve records verdicts for the rows a selector matches.
//
// Approving is the only way a verdict enters: documents are output (D3), so
// this is the single write path the review surface and the TUI both use.
func (s *Store) Approve(ctx context.Context, in ApproveInput) (*ApproveResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Actor) == "" {
		return nil, camperrors.NewValidation("actor", "is required", camperrors.ErrInvalidInput)
	}
	if in.Reject && in.Amend != "" {
		return nil, camperrors.NewValidation("amend",
			"cannot be combined with --reject: an amendment is an approval of a different disposition",
			camperrors.ErrInvalidInput)
	}

	run, err := s.OpenRun(ctx, in.RunID)
	if err != nil {
		return nil, err
	}
	verdicts, err := s.Verdicts(ctx, in.RunID)
	if err != nil {
		return nil, err
	}

	selection, err := ExpandSelector(run, verdicts, in.Selector)
	if err != nil {
		return nil, err
	}
	selection.Sort()
	if selection.Empty() {
		return nil, camperrors.Wrap(camperrors.ErrNotFound,
			"no rows matched "+in.Selector.Describe())
	}

	// Validate every amendment before recording any of them. A bulk amend that
	// failed on the third row would otherwise leave the first two written and
	// still return an error, so the operator could not tell from the failure
	// whether anything landed. Pre-flighting makes "it errored" mean "nothing
	// happened" for the whole class of failure a caller can actually cause.
	if in.Amend != "" {
		for _, row := range selection.Matched {
			if _, err := ResolveDisposition(TypePolicyFor(row.Type), in.Amend); err != nil {
				return nil, camperrors.Wrapf(err, "amend %s", row.StableID)
			}
		}
	}

	result := &ApproveResult{
		RunID:             in.RunID,
		Recorded:          []RecordedVerdict{},
		SkippedTerminal:   selection.TerminalIDs(),
		SkippedNoProposal: idsOf(selection.SkippedNoProposal),
	}

	for _, row := range selection.Matched {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		recorded, err := s.recordOne(ctx, in, row, verdicts[row.StableID])
		if err != nil {
			return nil, err
		}
		result.Recorded = append(result.Recorded, *recorded)
	}
	return result, nil
}

// recordOne writes the verdict for a single row.
func (s *Store) recordOne(ctx context.Context, in ApproveInput, row ManifestRow, current RowVerdict) (*RecordedVerdict, error) {
	disposition := current.Disposition
	action := current.CanonicalAction
	event := DecisionApproved

	switch {
	case in.Reject:
		event = DecisionRejected
	case in.Amend != "":
		// An amendment is a different disposition, so it is revalidated
		// against the row's own vocabulary rather than assumed compatible.
		resolved, err := ResolveDisposition(TypePolicyFor(row.Type), in.Amend)
		if err != nil {
			return nil, camperrors.Wrapf(err, "amend %s", row.StableID)
		}
		disposition = in.Amend
		action = resolved
		event = DecisionAmended
	}

	recorded := &RecordedVerdict{
		StableID:        row.StableID,
		Event:           string(event),
		Disposition:     disposition,
		CanonicalAction: action,
		ApplyCommand:    ApplyCommandFor(row, action),
	}

	// Re-approving a verdict that already stands is a no-op, not a second
	// event. A reviewer re-running a lane after fixing one row should not
	// double-write the rest, and the stream is an argument rather than a log
	// of keystrokes.
	if current.State == VerdictApproved &&
		event == DecisionApproved &&
		current.Disposition == disposition &&
		current.CanonicalAction == action {
		recorded.Unchanged = true
		return recorded, nil
	}

	if err := s.AppendDecision(ctx, in.RunID, DecisionEvent{
		Event:           event,
		StableID:        row.StableID,
		Disposition:     disposition,
		CanonicalAction: action,
		RationaleRef:    current.RationaleRef,
		Actor:           in.Actor,
		At:              in.Now,
		Note:            in.Note,
	}); err != nil {
		return nil, err
	}
	return recorded, nil
}

// ApplyCommandFor renders the command apply will run for a row.
//
// It is echoed back when a terminal verdict is recorded so the operator sees
// the actual mutation, not just a disposition label. The plan compiler in a
// later phase is the authority on what runs; this is the same mapping rendered
// for a human, and a test holds the two to the same vocabulary.
func ApplyCommandFor(row ManifestRow, action CanonicalAction) string {
	target := action.Target()
	switch action.Family() {
	case ActionFamilyAttention:
		return "camp workitem attention " + row.StableID + " --set " + target
	case ActionFamilyRail, ActionFamilyDungeon:
		return "camp workitem promote " + row.StableID + " --target " + target
	}
	switch action {
	case ActionSplit:
		return "camp workitem split " + row.StableID + " --into <successors>"
	case ActionNone:
		return ""
	}
	return ""
}

// idsOf lists rows by stable id.
func idsOf(rows []ManifestRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.StableID)
	}
	return ids
}
