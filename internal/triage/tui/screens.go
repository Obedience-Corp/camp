// Package tui renders and drives the lane-first review flow that
// `camp triage review` opens on a terminal.
//
// The screens are pure functions over run data, and the interaction runs on
// internal/crawl's two-step prompt grammar — the same one dungeon crawl uses,
// so the keys behave the way a camp user already expects. Splitting it this
// way means the *content* of every screen is testable without a terminal,
// while the terminal behavior itself is verified by driving the real binary in
// a pty (green unit tests do not verify a TUI).
//
// The flow records verdicts only. It never mutates a workitem: apply stays a
// separate explicit step whose preview the row card already showed.
//
// Reference: workflow/design/camp-triage/07-review-ux.md
package tui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/Obedience-Corp/camp/internal/triage"
)

// AnchorGlyph is the one-character staleness signal on a row card.
//
// Refresh writes real anchor verdicts in a later phase; until then every row
// renders unchecked rather than claiming a freshness nobody measured.
const (
	AnchorFresh     = "✓ fresh"
	AnchorUnchecked = "~ unchecked"
	AnchorChanged   = "! changed"
)

// Screen1 renders the run summary: the lane table, its counts, and the keys.
//
// It is a render of the same data `camp triage status --json` reports, so the
// screen and the machine surface cannot disagree.
func Screen1(run *triage.Run, status *triage.Status, lanes []triage.Lane) string {
	var b strings.Builder

	b.WriteString("TRIAGE " + run.ID + " · " + string(run.Manifest.Mode))
	if base := run.Manifest.BaseRunID; base != nil {
		b.WriteString(" (base: " + *base + ")")
	}
	b.WriteString("\n")
	b.WriteString("Profile: " + run.Manifest.Profile.Name + " · " +
		strconv.Itoa(status.Rows) + " items scoped · " +
		strconv.Itoa(status.Counts[string(triage.RowCarried)]) + " carried · " +
		strconv.Itoa(toReview(status)) + " to review\n\n")

	// The lane column is sized to the widest name actually present. A fixed
	// width silently misaligns the count column the moment a lane name grows
	// past it, which no unit test notices and every reader does.
	nameWidth := len("Lane")
	for _, lane := range lanes {
		if n := len([]rune(triage.LaneSelectorName(lane.Title))); n > nameWidth {
			nameWidth = n
		}
	}
	// One past the longest, so even the widest name pads rather than
	// overflowing into the next column.
	nameWidth++
	b.WriteString("  " + pad("Lane", nameWidth) + pad("rows", 6) + "state\n")
	for _, lane := range lanes {
		name := triage.LaneSelectorName(lane.Title)
		b.WriteString("  " + pad(name, nameWidth) + pad(strconv.Itoa(len(lane.Rows)), 6) + laneState(lane))
		if laneIsTerminal(lane) {
			b.WriteString("   ← terminal, confirms per row")
		}
		b.WriteString("\n")
	}

	if len(lanes) == 0 {
		b.WriteString("  (no rows in this run)\n")
	}

	b.WriteString("\n")
	b.WriteString(batchProgressLine(status))
	b.WriteString(stalenessLine(run))
	return b.String()
}

// toReview counts rows still awaiting a human verdict.
func toReview(status *triage.Status) int {
	total := 0
	for _, state := range []triage.RowState{
		triage.RowPendingEvidence, triage.RowProposed, triage.RowStale, triage.RowRejected,
	} {
		total += status.Counts[string(state)]
	}
	return total
}

// laneState summarizes a lane's verdicts the way the summary table shows them.
func laneState(lane triage.Lane) string {
	counts := map[triage.VerdictState]int{}
	for _, row := range lane.Rows {
		counts[row.Verdict.State]++
	}
	order := []struct {
		state triage.VerdictState
		label string
	}{
		{triage.VerdictProposed, "proposed"},
		{triage.VerdictApproved, "approved"},
		{triage.VerdictRejected, "rejected"},
		{triage.VerdictStale, "stale"},
		{triage.VerdictNone, "awaiting judgment"},
	}
	parts := make([]string, 0, len(order))
	for _, entry := range order {
		if n := counts[entry.state]; n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+entry.label)
		}
	}
	return strings.Join(parts, ", ")
}

// laneIsTerminal reports whether approving the lane would retire or split
// workitems, which is what makes it confirm per row.
func laneIsTerminal(lane triage.Lane) bool {
	for _, row := range lane.Rows {
		if row.Verdict.CanonicalAction.Terminal() {
			return true
		}
	}
	return false
}

// batchProgressLine makes a multi-sitting review legible.
func batchProgressLine(status *triage.Status) string {
	if len(status.Batches) <= 1 {
		return ""
	}
	decided := 0
	for _, batch := range status.Batches {
		if batch.Decided == batch.Rows {
			decided++
		}
	}
	return "  batches: " + strconv.Itoa(decided) + "/" + strconv.Itoa(len(status.Batches)) +
		" complete\n"
}

// stalenessLine surfaces a stale refresh at the top rather than burying it.
func stalenessLine(run *triage.Run) string {
	if run.State.Phase == triage.PhaseVerified || run.State.Phase == triage.PhaseAbandoned {
		return ""
	}
	return "  anchors: " + AnchorUnchecked +
		" (run `camp triage refresh` once it lands to re-check them)\n"
}

// Screen2 renders a lane view: one line per row.
func Screen2(lane triage.Lane, batchFilter int) string {
	var b strings.Builder

	b.WriteString(strings.ToUpper(lane.Title) + " · " +
		strconv.Itoa(len(lane.Rows)) + " rows\n")
	if batchFilter > 0 {
		b.WriteString("batch " + strconv.Itoa(batchFilter) + "\n")
	}
	b.WriteString(lane.Summary + "\n\n")

	rows := visibleRows(lane, batchFilter)
	if len(rows) == 0 {
		b.WriteString("  (no rows)\n")
		return b.String()
	}
	for i, row := range rows {
		b.WriteString("  " + pad(strconv.Itoa(i+1)+".", 4) + RowLine(row) + "\n")
	}
	return b.String()
}

// visibleRows applies the batch filter a large run pages by.
func visibleRows(lane triage.Lane, batchFilter int) []triage.LaneRow {
	if batchFilter <= 0 {
		return lane.Rows
	}
	out := make([]triage.LaneRow, 0, len(lane.Rows))
	for _, row := range lane.Rows {
		if row.Row.Batch == batchFilter {
			out = append(out, row)
		}
	}
	return out
}

// RowLine is the one-line card a lane view lists.
func RowLine(row triage.LaneRow) string {
	parts := []string{
		pad(truncate(row.Row.Title, 40), 42),
		pad(row.Row.Type, 10),
	}
	if row.Verdict.Disposition != "" {
		parts = append(parts, pad(row.Verdict.Disposition, 14))
	} else {
		parts = append(parts, pad("—", 14))
	}
	parts = append(parts, pad(confidenceOf(row), 8), AnchorUnchecked)
	return strings.Join(parts, " ")
}

// confidenceOf reads the recorded rationale's confidence, which is the number
// a reviewer actually weighs.
func confidenceOf(row triage.LaneRow) string {
	if row.Rationale != nil && row.Rationale.Confidence != "" {
		return string(row.Rationale.Confidence)
	}
	return "—"
}

// CardInput is everything a row card renders.
type CardInput struct {
	Row      triage.LaneRow
	Position int
	Total    int
	Lane     string
	// Evidence is the row's stored record, when it has one.
	Evidence *triage.EvidenceRecord
	// Successors are a consolidate row's declared successors with their
	// exists state, rendered inline as doc 06's queue.
	Successors []Successor
	// Compact renders the sweep-profile variant: no evidence block, so an
	// inbox-intent pass moves at seconds per row.
	Compact bool
}

// Successor is one declared successor of a consolidation.
type Successor struct {
	StableID string
	Exists   bool
}

// Screen3 renders the row card.
func Screen3(in CardInput) string {
	var b strings.Builder
	row := in.Row

	b.WriteString("[" + in.Lane + " " + strconv.Itoa(in.Position) + "/" +
		strconv.Itoa(in.Total) + "]  " + row.Row.StableID)
	b.WriteString("        " + row.Row.Type)
	if row.Row.Ref != "" {
		b.WriteString(" · " + row.Row.Ref)
	}
	b.WriteString("\n")

	if row.Verdict.Disposition != "" {
		b.WriteString("Proposed: " + row.Verdict.Disposition + " → " +
			string(row.Verdict.CanonicalAction))
		b.WriteString("          confidence: " + confidenceOf(row) +
			" · anchors " + AnchorUnchecked + "\n\n")
	} else {
		b.WriteString("Proposed: nothing yet\n\n")
	}

	if !in.Compact {
		writeEvidenceTrio(&b, in)
	}

	if command := triage.ApplyCommandFor(row.Row, row.Verdict.CanonicalAction); command != "" {
		b.WriteString("  Will run:  " + command + "\n")
		if row.Verdict.CanonicalAction.Terminal() {
			b.WriteString("             this is terminal and confirms individually\n")
		}
		b.WriteString("\n")
	}

	if len(in.Successors) > 0 {
		b.WriteString("  Successors:\n")
		for _, successor := range in.Successors {
			state := "missing"
			if successor.Exists {
				state = "exists"
			}
			b.WriteString("    " + pad(successor.StableID, 44) + state + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeEvidenceTrio renders the three lines that drive the decision, never the
// full record: the card is a decision surface, not a ledger dump.
func writeEvidenceTrio(b *strings.Builder, in CardInput) {
	record := in.Evidence
	if record == nil {
		b.WriteString("  (no evidence record)\n\n")
		return
	}
	if record.NoEvidence {
		b.WriteString("  Judged without a gathered record.\n\n")
		return
	}
	b.WriteString("  Delivered: " + summarize(record.Delivered) + "\n")
	b.WriteString("  Missing:   " + summarize(record.Missing) + "\n")
	b.WriteString("  Follow-up: " + summarize(record.OpenDecisions) + "\n\n")
}

// summarize joins a list into one card line, truncating rather than wrapping.
func summarize(values []string) string {
	if len(values) == 0 {
		return "—"
	}
	return truncate(strings.Join(values, " · "), 70)
}

// SuccessorsFor reads a consolidate row's declared successors from the run's
// other rows, so the card can show what exists without a discovery walk.
func SuccessorsFor(run *triage.Run, row triage.LaneRow) []Successor {
	if row.Verdict.CanonicalAction != triage.ActionSplit {
		return nil
	}
	if row.Rationale == nil || len(row.Rationale.AnchorsUsed) == 0 {
		return nil
	}
	present := map[string]bool{}
	for _, manifestRow := range run.Manifest.Rows {
		present[manifestRow.StableID] = true
	}
	out := make([]Successor, 0, len(row.Rationale.AnchorsUsed))
	for _, id := range row.Rationale.AnchorsUsed {
		out = append(out, Successor{StableID: id, Exists: present[id]})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].StableID < out[b].StableID })
	return out
}

// --- small helpers -----------------------------------------------------

// pad right-pads to width, always leaving at least one separating space so a
// value at or past the width never runs into the next column.
func pad(s string, width int) string {
	runes := len([]rune(s))
	if runes >= width {
		return s + " "
	}
	return s + strings.Repeat(" ", width-runes)
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}
