package triage

import (
	"sort"
	"strconv"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Selector names which rows a verdict applies to.
//
// Exactly one form is set. Stable ids are an explicit list; lane and batch are
// bulk forms, and the difference matters because bulk selection deliberately
// refuses to cover terminal rows.
type Selector struct {
	StableIDs []string
	Lane      string
	Batch     int
}

// Bulk reports whether the selector selects by group rather than by name.
func (s Selector) Bulk() bool { return s.Lane != "" || s.Batch > 0 }

// Describe renders the selector for a message.
func (s Selector) Describe() string {
	switch {
	case s.Lane != "":
		return "lane " + quote(s.Lane)
	case s.Batch > 0:
		return "batch " + strconv.Itoa(s.Batch)
	case len(s.StableIDs) == 1:
		return quote(s.StableIDs[0])
	default:
		return strconv.Itoa(len(s.StableIDs)) + " rows"
	}
}

// Selection is the result of expanding a selector against a run.
type Selection struct {
	// Matched are the rows a verdict will be recorded for.
	Matched []ManifestRow
	// SkippedTerminal are rows a bulk selector deliberately did not cover
	// because their action is irreversible without a human naming them.
	SkippedTerminal []ManifestRow
	// SkippedNoProposal are rows with nothing to approve.
	SkippedNoProposal []ManifestRow
}

// TerminalIDs lists the skipped terminal rows by id, in order.
func (s Selection) TerminalIDs() []string {
	ids := make([]string, 0, len(s.SkippedTerminal))
	for _, row := range s.SkippedTerminal {
		ids = append(ids, row.StableID)
	}
	return ids
}

// LaneNames returns the lane names a selector may address, in priority order.
func LaneNames(run *Run, verdicts map[string]RowVerdict) []string {
	lanes := BuildLanes(run, verdicts, nil)
	names := make([]string, 0, len(lanes))
	for _, lane := range lanes {
		names = append(names, laneSelectorName(lane.Title))
	}
	return names
}

// laneSelectorName turns a lane heading into the token a user types.
func laneSelectorName(title string) string {
	return LaneSelectorName(title)
}

// LaneSelectorName is the selector token for a lane heading. Exported because
// the review flow labels its lane list with the same tokens the CLI accepts,
// so what a user reads on screen is what they can type.
func LaneSelectorName(title string) string {
	return strings.ReplaceAll(strings.ToLower(title), " ", "-")
}

// ExpandSelector resolves a selector to the rows a verdict applies to.
//
// The rule that matters is the terminal exclusion. A bulk selector never
// covers a row whose action retires or splits a workitem: those are
// irreversible enough that the operator has to name them, and the field trial
// showed a reviewer approving a batch is not meaningfully consenting to each
// terminal row inside it. Naming a terminal row individually is allowed, and
// the caller echoes exactly what apply will run.
//
// Pure over (manifest, fold), so the rule is testable without a filesystem.
func ExpandSelector(run *Run, verdicts map[string]RowVerdict, selector Selector) (*Selection, error) {
	switch {
	case len(selector.StableIDs) > 0:
		return expandByID(run, verdicts, selector.StableIDs)
	case selector.Lane != "":
		return expandByLane(run, verdicts, selector.Lane)
	case selector.Batch > 0:
		return expandByBatch(run, verdicts, selector.Batch)
	default:
		return nil, camperrors.NewValidation("selector",
			"name a stable id, or use --lane or --batch", camperrors.ErrInvalidInput)
	}
}

// expandByID resolves explicitly named rows. Naming a row is consent, so a
// terminal row selected this way is matched rather than skipped.
func expandByID(run *Run, verdicts map[string]RowVerdict, ids []string) (*Selection, error) {
	selection := &Selection{}
	var unknown []string

	for _, id := range ids {
		row, err := run.Row(id)
		if err != nil {
			unknown = append(unknown, id)
			continue
		}
		if !HasLiveProposal(verdicts[id]) {
			selection.SkippedNoProposal = append(selection.SkippedNoProposal, *row)
			continue
		}
		selection.Matched = append(selection.Matched, *row)
	}

	if len(unknown) > 0 {
		return nil, camperrors.NewValidation("selector",
			"no row in this run named "+strings.Join(unknown, ", "), camperrors.ErrInvalidInput)
	}
	return selection, nil
}

// expandByLane resolves every row in a lane, minus the terminal ones.
func expandByLane(run *Run, verdicts map[string]RowVerdict, lane string) (*Selection, error) {
	want := laneSelectorName(lane)
	lanes := BuildLanes(run, verdicts, nil)

	var found bool
	selection := &Selection{}
	for _, candidate := range lanes {
		if laneSelectorName(candidate.Title) != want {
			continue
		}
		found = true
		for _, laneRow := range candidate.Rows {
			addBulkRow(selection, laneRow.Row, verdicts[laneRow.Row.StableID])
		}
	}
	if !found {
		return nil, camperrors.NewValidation("lane",
			"no lane named "+quote(lane)+" in this run; available: "+
				strings.Join(LaneNames(run, verdicts), ", "), camperrors.ErrInvalidInput)
	}
	return selection, nil
}

// expandByBatch resolves every row in a review batch, minus the terminal ones.
func expandByBatch(run *Run, verdicts map[string]RowVerdict, batch int) (*Selection, error) {
	selection := &Selection{}
	var found bool
	for _, row := range run.Manifest.Rows {
		if row.Batch != batch {
			continue
		}
		found = true
		addBulkRow(selection, row, verdicts[row.StableID])
	}
	if !found {
		return nil, camperrors.NewValidation("batch",
			"no batch "+strconv.Itoa(batch)+" in this run; it has "+
				strconv.Itoa(BatchCount(run.Manifest))+" batches", camperrors.ErrInvalidInput)
	}
	return selection, nil
}

// addBulkRow applies the bulk rules to one row.
func addBulkRow(selection *Selection, row ManifestRow, verdict RowVerdict) {
	if !HasLiveProposal(verdict) {
		selection.SkippedNoProposal = append(selection.SkippedNoProposal, row)
		return
	}
	if verdict.CanonicalAction.Terminal() {
		selection.SkippedTerminal = append(selection.SkippedTerminal, row)
		return
	}
	selection.Matched = append(selection.Matched, row)
}

// Sort orders every slice by stable id so output and recorded events are
// stable regardless of manifest order.
func (s *Selection) Sort() {
	for _, rows := range [][]ManifestRow{s.Matched, s.SkippedTerminal, s.SkippedNoProposal} {
		sort.Slice(rows, func(a, b int) bool { return rows[a].StableID < rows[b].StableID })
	}
}

// Empty reports whether the selector matched nothing at all — not one row to
// record, skip, or report.
func (s *Selection) Empty() bool {
	return len(s.Matched) == 0 && len(s.SkippedTerminal) == 0 && len(s.SkippedNoProposal) == 0
}
