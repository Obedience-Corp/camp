package triage

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Rendered document names inside a run.
const (
	ReviewDocFileName     = "TRIAGE_REVIEW.md"
	PrioritiesDocFileName = "PRIORITIES.md"
)

// RenderInput is everything the renderers read. Nothing else is consulted —
// in particular there is no clock, so re-rendering unchanged data produces
// identical bytes and a no-op diff.
type RenderInput struct {
	Run        *Run
	Verdicts   map[string]RowVerdict
	Rationales map[string]*Rationale
	// EvidenceRoles counts stored evidence records by producer role, for the
	// method section. Derived from the run, not measured at render time.
	EvidenceRoles map[EvidenceRole]int
	// NoEvidenceCount is how many rows were judged without a gathered record.
	NoEvidenceCount int
}

// generatedBanner is the D3 statement, rendered into every document.
//
// Approval is CLI-recorded and documents are strictly output. Saying so in the
// file is the difference between a user trusting an edit and losing it: the
// next render overwrites whatever they wrote.
func generatedBanner(runID string) string {
	return "> Generated from run `" + runID + "` by `camp triage review`.\n" +
		"> **Do not edit.** Verdicts are recorded with `camp triage approve`;\n" +
		"> re-rendering replaces this file and any edits in it are lost.\n"
}

// RenderReview renders TRIAGE_REVIEW.md: the canonical human surface.
func RenderReview(in RenderInput) []byte {
	var b strings.Builder
	run := in.Run
	lanes := BuildLanes(run, in.Verdicts, in.Rationales)

	b.WriteString("# Triage review — " + run.ID + "\n\n")
	b.WriteString(generatedBanner(run.ID))
	b.WriteString("\n")

	b.WriteString("**Run:** `" + run.ID + "`  \n")
	b.WriteString("**Phase:** " + string(run.State.Phase) + "  \n")
	b.WriteString("**Profile:** `" + run.Manifest.Profile.Name + "` (" +
		string(run.Manifest.Mode) + " run)  \n")
	b.WriteString("**Snapshot:** " + plural(len(run.Manifest.Rows), "row", "rows") +
		" in " + plural(BatchCount(run.Manifest), "batch", "batches") +
		", taken " + stamp(run.Manifest.CreatedAt) + "\n\n")

	writeDecisionRequested(&b, lanes)
	writePriorityOrder(&b, lanes)
	writePortfolioDecisions(&b, lanes)
	writeResultingShape(&b, lanes)
	writeMethod(&b, in)
	writeApprovalRecord(&b, in)

	return []byte(b.String())
}

// writeDecisionRequested states what approval authorizes, and what it does not.
func writeDecisionRequested(b *strings.Builder, lanes []Lane) {
	b.WriteString("## Decision requested\n\n")
	b.WriteString("Review the proposed portfolio below. Approve it as a batch, approve\n")
	b.WriteString("selected lanes, or amend individual rows:\n\n")
	b.WriteString("```\ncamp triage approve --lane <lane>\n")
	b.WriteString("camp triage approve <stable-id> --amend <disposition>\n")
	b.WriteString("camp triage approve <stable-id> --reject --note \"why\"\n```\n\n")
	b.WriteString("Approval authorizes the recorded actions and nothing else. No workitem\n")
	b.WriteString("is deleted: terminal dispositions move a workitem into the dungeon,\n")
	b.WriteString("where it stays readable. A consolidated parent is not retired until\n")
	b.WriteString("every successor it declared exists.\n\n")

	if undecided := laneByKey(lanes, laneUndecided); undecided != nil {
		b.WriteString("**" + plural(len(undecided.Rows), "row has", "rows have") +
			" no proposal yet.** They are listed at the end and are not covered by\n")
		b.WriteString("any approval below.\n\n")
	}
}

// writePriorityOrder presents the lanes in the order they should be read.
func writePriorityOrder(b *strings.Builder, lanes []Lane) {
	b.WriteString("## Recommended priority order\n\n")
	if len(lanes) == 0 {
		b.WriteString("Nothing in this run.\n\n")
		return
	}
	position := 0
	for _, lane := range lanes {
		if lane.Key == laneUndecided {
			continue
		}
		position++
		b.WriteString(strconv.Itoa(position) + ". **" + lane.Title + "** — " +
			plural(len(lane.Rows), "row", "rows") + ". " + lane.Summary + "\n")
	}
	if position == 0 {
		b.WriteString("No decisions recorded yet.\n")
	}
	b.WriteString("\n")
}

// writePortfolioDecisions renders one table per lane.
func writePortfolioDecisions(b *strings.Builder, lanes []Lane) {
	b.WriteString("## Proposed portfolio decisions\n\n")
	for _, lane := range lanes {
		b.WriteString("### " + lane.Title + "\n\n")
		b.WriteString(lane.Summary + "\n\n")

		if lane.Key == laneUndecided {
			b.WriteString("| Workitem | Type | Batch |\n| --- | --- | ---: |\n")
			for _, row := range lane.Rows {
				b.WriteString("| `" + row.Row.StableID + "` | " + row.Row.Type +
					" | " + strconv.Itoa(row.Row.Batch) + " |\n")
			}
			b.WriteString("\n")
			continue
		}

		b.WriteString("| Workitem | Disposition | Action | Rationale |\n")
		b.WriteString("| --- | --- | --- | --- |\n")
		for _, row := range lane.Rows {
			b.WriteString("| `" + row.Row.StableID + "` | " +
				row.Verdict.Disposition + " | `" + string(row.Verdict.CanonicalAction) +
				"` | " + rationaleCell(row) + " |\n")
		}
		b.WriteString("\n")
	}

	if exceptions := identityExceptionRows(lanes); len(exceptions) > 0 {
		b.WriteString("### Identity exceptions\n\n")
		b.WriteString(plural(len(exceptions), "row", "rows") + " " +
			verb(len(exceptions), "is", "are") +
			" identified only by path: no `.workitem` marker resolved during\n")
		b.WriteString("preflight, so the path is the identity. A move invalidates it.\n\n")
		for _, id := range exceptions {
			b.WriteString("- `" + id + "`\n")
		}
		b.WriteString("\n")
	}
}

// writeResultingShape is the count table the trial proved useful.
func writeResultingShape(b *strings.Builder, lanes []Lane) {
	b.WriteString("## Resulting portfolio shape\n\n")
	b.WriteString("| Result | Count |\n| --- | ---: |\n")
	total := 0
	for _, lane := range lanes {
		b.WriteString("| " + lane.Title + " | " + strconv.Itoa(len(lane.Rows)) + " |\n")
		total += len(lane.Rows)
	}
	b.WriteString("| **Total** | **" + strconv.Itoa(total) + "** |\n\n")
}

// writeMethod describes how the result was produced, from the run's own
// recorded history rather than from a narrative someone wrote.
func writeMethod(b *strings.Builder, in RenderInput) {
	run := in.Run
	b.WriteString("## How this result was produced\n\n")
	b.WriteString("1. Snapshotted " + plural(len(run.Manifest.Rows), "row", "rows") +
		" under profile `" + run.Manifest.Profile.Name + "`, partitioned into " +
		plural(BatchCount(run.Manifest), "batch", "batches") +
		" by `" + string(run.Manifest.Profile.Resolved.Review.GroupBy) + "`.\n")
	b.WriteString("2. Gathered evidence per row at the depth its lane's policy asked for.\n")
	b.WriteString("3. Recorded one proposal per row, resolved through the row's type\n")
	b.WriteString("   vocabulary into the action camp will perform.\n")
	b.WriteString("4. Stopped here, at the approval checkpoint, before any mutation.\n\n")

	b.WriteString("### Phase history\n\n")
	b.WriteString("| Phase | Recorded at |\n| --- | --- |\n")
	for _, transition := range run.State.PhaseHistory {
		b.WriteString("| " + string(transition.Phase) + " | " + stamp(transition.At) + " |\n")
	}
	b.WriteString("\n")

	b.WriteString("### Evidence\n\n")
	if len(in.EvidenceRoles) == 0 && in.NoEvidenceCount == 0 {
		b.WriteString("No evidence records stored yet.\n\n")
	} else {
		b.WriteString("| Produced by | Records |\n| --- | ---: |\n")
		for _, role := range sortedRoles(in.EvidenceRoles) {
			b.WriteString("| " + role + " | " + strconv.Itoa(in.EvidenceRoles[EvidenceRole(role)]) + " |\n")
		}
		if in.NoEvidenceCount > 0 {
			b.WriteString("| judged without a gathered record | " +
				strconv.Itoa(in.NoEvidenceCount) + " |\n")
		}
		b.WriteString("\n")
		b.WriteString("Camp calls no models. Every record above was submitted through\n")
		b.WriteString("`camp triage evidence set` and validated against the\n")
		b.WriteString("`" + SchemaVersion + "` schema before it was stored.\n\n")
	}
}

// writeApprovalRecord renders the verdict events, which is the authoritative
// decision record. An empty record says so rather than implying consent.
func writeApprovalRecord(b *strings.Builder, in RenderInput) {
	b.WriteString("## Approval record\n\n")

	type decided struct {
		id      string
		verdict RowVerdict
	}
	var rows []decided
	for id, verdict := range in.Verdicts {
		switch verdict.State {
		case VerdictApproved, VerdictRejected:
			rows = append(rows, decided{id, verdict})
		}
	}
	if len(rows) == 0 {
		b.WriteString("Nothing approved or rejected yet. Every row above is a proposal.\n")
		return
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].id < rows[b].id })

	b.WriteString("| Workitem | Verdict | Disposition | Decided by | Decided at |\n")
	b.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, row := range rows {
		b.WriteString("| `" + row.id + "` | " + string(row.verdict.State) + " | " +
			row.verdict.Disposition + " | " + row.verdict.Actor + " | " +
			stamp(row.verdict.At) + " |\n")
	}
	b.WriteString("\n")
}

// RenderPriorities renders PRIORITIES.md: the answer to "what should I work
// on", which is the artifact that keeps working after the session ends.
func RenderPriorities(in RenderInput) []byte {
	var b strings.Builder
	run := in.Run
	lanes := BuildLanes(run, in.Verdicts, in.Rationales)

	b.WriteString("# Portfolio priorities\n\n")
	b.WriteString(generatedBanner(run.ID))
	b.WriteString("\n")
	b.WriteString("**Snapshot taken:** " + stamp(run.Manifest.CreatedAt) + "  \n")
	b.WriteString("**Phase:** " + string(run.State.Phase) + "\n\n")

	if run.State.Phase != PhaseVerified {
		b.WriteString("**Authority:** proposals and recorded verdicts. Nothing below has\n")
		b.WriteString("been applied yet.\n\n")
	}

	writeLaneNarrative(&b, lanes, laneCurrent, "Primary execution thread",
		"Work happening now. Finish this before starting anything large.")
	writeLaneNarrative(&b, lanes, laneNext, "Next up",
		"Queued behind the current lane.")
	writeLaneNarrative(&b, lanes, laneActive, "Active, outside the current focus",
		"Live work that is not the primary thread.")
	writeLaneNarrative(&b, lanes, laneRail, "Promoted onto the festival rail",
		"Moving into planned execution.")
	writeLaneNarrative(&b, lanes, laneParked, "Parked",
		"Kept and visible, deliberately not being worked.")
	writeLaneNarrative(&b, lanes, laneKeep, "Reviewed, left as is",
		"Looked at and deliberately unchanged.")

	b.WriteString("## Portfolio cleanup\n\n")
	cleanup := 0
	for _, key := range []int{laneCompleted, laneArchived, laneSomeday, laneConsolidate} {
		if lane := laneByKey(lanes, key); lane != nil {
			cleanup += len(lane.Rows)
			b.WriteString("**" + lane.Title + "**\n\n")
			for _, row := range lane.Rows {
				b.WriteString("- `" + row.Row.StableID + "`")
				if summary := rationaleSummary(row); summary != "" {
					b.WriteString(" — " + summary)
				}
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
	}
	if cleanup == 0 {
		b.WriteString("Nothing to retire from this run.\n\n")
	}

	if undecided := laneByKey(lanes, laneUndecided); undecided != nil {
		b.WriteString("## Not yet decided\n\n")
		b.WriteString(plural(len(undecided.Rows), "row", "rows") +
			" in this run " + verb(len(undecided.Rows), "has", "have") +
			" no proposal yet:\n\n")
		for _, row := range undecided.Rows {
			b.WriteString("- `" + row.Row.StableID + "`\n")
		}
		b.WriteString("\n")
	}

	return []byte(b.String())
}

// writeLaneNarrative renders one lane as prose bullets, omitting it entirely
// when empty so the brief stays short.
func writeLaneNarrative(b *strings.Builder, lanes []Lane, key int, title, blurb string) {
	lane := laneByKey(lanes, key)
	if lane == nil || len(lane.Rows) == 0 {
		return
	}
	b.WriteString("## " + title + "\n\n")
	b.WriteString(blurb + "\n\n")
	for _, row := range lane.Rows {
		b.WriteString("- `" + row.Row.StableID + "`")
		if row.Row.Title != "" {
			b.WriteString(" — " + row.Row.Title)
		}
		if summary := rationaleSummary(row); summary != "" {
			b.WriteString(". " + summary)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

// --- helpers -----------------------------------------------------------

func laneByKey(lanes []Lane, key int) *Lane {
	for i := range lanes {
		if lanes[i].Key == key {
			return &lanes[i]
		}
	}
	return nil
}

// rationaleCell renders a rationale for a table cell, escaping the pipe that
// would otherwise break the column.
func rationaleCell(row LaneRow) string {
	summary := rationaleSummary(row)
	if summary == "" {
		return "—"
	}
	return strings.ReplaceAll(summary, "|", "\\|")
}

func rationaleSummary(row LaneRow) string {
	if row.Rationale != nil && row.Rationale.Summary != "" {
		return collapseWhitespace(row.Rationale.Summary)
	}
	if row.Verdict.Note != "" {
		return collapseWhitespace(row.Verdict.Note)
	}
	return ""
}

// identityExceptionRows lists rows whose identity is path-bound, in id order.
func identityExceptionRows(lanes []Lane) []string {
	var ids []string
	for _, lane := range lanes {
		for _, row := range lane.Rows {
			if row.Row.IdentityException != nil {
				ids = append(ids, row.Row.StableID)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

func sortedRoles(counts map[EvidenceRole]int) []string {
	roles := make([]string, 0, len(counts))
	for role := range counts {
		roles = append(roles, string(role))
	}
	sort.Strings(roles)
	return roles
}

// stamp renders a recorded timestamp. Renderers never read a clock, so every
// timestamp in a document comes from the run.
func stamp(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}

// verb agrees with a count, so a rendered sentence reads correctly at one.
func verb(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// plural renders "1 row" / "3 rows".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// collapseWhitespace flattens a multi-line rationale so it fits a table cell
// without breaking the row.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
