package triage

import "sort"

// Lane is one section of a rendered review: the rows whose verdicts resolve to
// the same kind of outcome.
//
// Lanes partition the manifest. Every row lands in exactly one, including rows
// nobody has judged — a review that quietly omitted them would be a document
// the operator could approve without ever seeing what it left out.
type Lane struct {
	// Key orders lanes; lower sorts first.
	Key int
	// Title is the section heading.
	Title string
	// Summary explains what approving this lane does.
	Summary string
	Rows    []LaneRow
}

// LaneRow is one row as it appears in a rendered document.
type LaneRow struct {
	Row     ManifestRow
	Verdict RowVerdict
	// Rationale is the recorded justification, when the proposal referenced one.
	Rationale *Rationale
}

// Lane ordering. The sequence is the priority order the review presents:
// what is being worked now, what is next, what is being kept, what is being
// retired, and finally what has not been decided at all.
const (
	laneCurrent = iota
	laneNext
	laneActive
	laneRail
	laneKeep
	laneParked
	laneCompleted
	laneArchived
	laneSomeday
	laneConsolidate
	laneUndecided
)

// laneSpec describes one lane's presentation.
type laneSpec struct {
	key     int
	title   string
	summary string
}

// laneSpecFor maps a canonical action to its lane. A row with no live verdict
// falls to the undecided lane regardless of what it once held.
func laneSpecFor(verdict RowVerdict) laneSpec {
	if !HasLiveProposal(verdict) {
		return laneSpec{laneUndecided, "Awaiting judgment",
			"No live proposal. These rows are in the run but nothing has been decided for them."}
	}
	switch verdict.CanonicalAction {
	case "attention/current":
		return laneSpec{laneCurrent, "Current",
			"Work happening now. Applying sets the attention stage; nothing is moved or retired."}
	case "attention/next":
		return laneSpec{laneNext, "Next",
			"Queued behind the current lane. Applying sets the attention stage only."}
	case "attention/active":
		return laneSpec{laneActive, "Active",
			"Live work outside the current focus. Applying sets the attention stage only."}
	case "attention/parked":
		return laneSpec{laneParked, "Park for later",
			"Kept and visible, deliberately not being worked. Recoverable at any time."}
	case ActionNone:
		return laneSpec{laneKeep, "Keep as is",
			"Reviewed and left alone. Applying changes nothing for these rows."}
	case ActionSplit:
		return laneSpec{laneConsolidate, "Consolidate and retire",
			"Each parent is split into focused successors first. A parent is not retired until every declared successor exists."}
	}
	switch verdict.CanonicalAction.Family() {
	case ActionFamilyRail:
		return laneSpec{laneRail, "Promote onto the festival rail",
			"Promoted along the workitem rail. Forward-only; approval is required."}
	case ActionFamilyDungeon:
		return laneSpec{dungeonLaneKey(verdict.CanonicalAction.Target()),
			dungeonLaneTitle(verdict.CanonicalAction.Target()),
			"Terminal. Nothing is deleted: the workitem moves to the dungeon and stays readable."}
	}
	return laneSpec{laneUndecided, "Awaiting judgment",
		"No live proposal. These rows are in the run but nothing has been decided for them."}
}

func dungeonLaneKey(target string) int {
	switch target {
	case "completed":
		return laneCompleted
	case "archived":
		return laneArchived
	default:
		return laneSomeday
	}
}

func dungeonLaneTitle(target string) string {
	switch target {
	case "completed":
		return "Close as delivered"
	case "archived":
		return "Archive"
	default:
		return "Someday"
	}
}

// BuildLanes partitions a run's rows into ordered lanes.
//
// The partition is total and disjoint by construction: every manifest row is
// visited once and assigned exactly one lane. `TestEveryRowAppearsExactlyOnce`
// holds this to it.
func BuildLanes(run *Run, verdicts map[string]RowVerdict, rationales map[string]*Rationale) []Lane {
	byKey := map[int]*Lane{}
	for _, row := range run.Manifest.Rows {
		verdict := verdicts[row.StableID]
		spec := laneSpecFor(verdict)
		lane, ok := byKey[spec.key]
		if !ok {
			lane = &Lane{Key: spec.key, Title: spec.title, Summary: spec.summary}
			byKey[spec.key] = lane
		}
		lane.Rows = append(lane.Rows, LaneRow{
			Row:       row,
			Verdict:   verdict,
			Rationale: rationales[row.StableID],
		})
	}

	lanes := make([]Lane, 0, len(byKey))
	for _, lane := range byKey {
		// Rows sort by stable id so a re-render never reorders a table.
		sort.Slice(lane.Rows, func(a, b int) bool {
			return lane.Rows[a].Row.StableID < lane.Rows[b].Row.StableID
		})
		lanes = append(lanes, *lane)
	}
	sort.Slice(lanes, func(a, b int) bool { return lanes[a].Key < lanes[b].Key })
	return lanes
}
