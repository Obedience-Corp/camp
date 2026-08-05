package triage

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// updateGoldens rewrites the golden files instead of comparing against them.
// Run with `go test ./internal/triage/ -update` after a deliberate change to
// the document shape, then read the diff before committing it.
var updateGoldens = flag.Bool("update", false, "rewrite render goldens")

// renderFixture builds a run whose rows exercise every lane, modeled on the
// 2026-08-03 field trial's real review: an execution thread, correctness work
// queued next, items kept active, items parked, items closed as delivered, and
// a consolidation set — plus rows nobody judged, which the trial also had.
func renderFixture() RenderInput {
	manifest := &Manifest{
		SchemaVersion: SchemaVersion,
		RunID:         "run-20260810T140000Z",
		Mode:          RunModeFull,
		Profile:       ManifestProfile{Name: ProfileNameDefault, Resolved: DefaultProfile()},
		CreatedAt:     testAt,
	}

	type spec struct {
		id, title, wfType string
		batch             int
		exception         bool
	}
	specs := []spec{
		{id: "camp-intent-notes-tui", title: "Camp intent notes TUI", wfType: "design", batch: 1},
		{id: "obey-observation-boundary", title: "Obey observation boundary", wfType: "design", batch: 1},
		{id: "obey-machine-session-boundary", title: "Obey machine-session boundary", wfType: "design", batch: 1},
		{id: "fest-ritual-creation-lifecycle", title: "Fest ritual creation lifecycle", wfType: "design", batch: 2},
		{id: "loop-scheduling-primitives", title: "Loop scheduling primitives", wfType: "design", batch: 2},
		{id: "camp-artifact-commit-updates", title: "Camp artifact commit updates", wfType: "design", batch: 2},
		{id: "platform-adoption-and-extensibility", title: "Platform adoption and extensibility", wfType: "design", batch: 3},
		{id: "festival-hub-control-plane", title: "Festival hub control plane", wfType: "design", batch: 3, exception: true},
		{id: "shared-template-sync", title: "Shared template sync", wfType: "design", batch: 3},
		{id: "intent-tidy-the-inbox", title: "Tidy the inbox", wfType: "intent", batch: 4},
	}
	for _, s := range specs {
		row := ManifestRow{
			StableID:       s.id,
			Ref:            "WI-" + s.id[:6],
			Key:            s.wfType + ":workflow/" + s.wfType + "/" + s.id,
			Type:           s.wfType,
			Title:          s.title,
			RelativePath:   "workflow/" + s.wfType + "/" + s.id,
			LifecycleStage: "active",
			AttentionStage: "active",
			Batch:          s.batch,
			Policy:         RowPolicy{Evidence: EvidenceDepthDeep, RoutingTier: RoutingTierDefault},
		}
		if s.exception {
			row.IdentityException = &IdentityException{
				Reason:    "no .workitem marker: identity is bound to the path until adopted",
				Path:      row.RelativePath,
				GrantedBy: "camp-triage-preflight",
				GrantedAt: testAt,
			}
		}
		manifest.Rows = append(manifest.Rows, row)
	}
	manifest.Normalize()

	run := &Run{
		ID:       manifest.RunID,
		Dir:      "/campaign/.campaign/triage/runs/" + manifest.RunID,
		Manifest: manifest,
		State: &RunState{
			SchemaVersion: SchemaVersion,
			RunID:         manifest.RunID,
			Phase:         PhaseReviewing,
			PhaseHistory: []PhaseTransition{
				{Phase: PhaseCreated, At: testAt},
				{Phase: PhaseSnapshotted, At: testAt.Add(60)},
				{Phase: PhaseJudging, At: testAt.Add(120)},
				{Phase: PhaseReviewing, At: testAt.Add(180)},
			},
		},
	}

	verdict := func(state VerdictState, disposition string, action CanonicalAction) RowVerdict {
		return RowVerdict{
			State: state, Disposition: disposition, CanonicalAction: action,
			Actor: "lancekrogers", At: testAt.Add(180), Events: 2,
		}
	}
	verdicts := map[string]RowVerdict{
		"camp-intent-notes-tui":               verdict(VerdictApproved, "current", "attention/current"),
		"obey-observation-boundary":           verdict(VerdictProposed, "next", "attention/next"),
		"obey-machine-session-boundary":       verdict(VerdictProposed, "next", "attention/next"),
		"fest-ritual-creation-lifecycle":      verdict(VerdictProposed, "active", "attention/active"),
		"loop-scheduling-primitives":          verdict(VerdictProposed, "parked", "attention/parked"),
		"camp-artifact-commit-updates":        verdict(VerdictApproved, "completed", "dungeon/completed"),
		"platform-adoption-and-extensibility": verdict(VerdictProposed, "consolidate", ActionSplit),
		"festival-hub-control-plane":          verdict(VerdictProposed, "ready", "rail/ready"),
		"shared-template-sync":                verdict(VerdictRejected, "parked", "attention/parked"),
		// intent-tidy-the-inbox is deliberately left unjudged.
	}

	rationales := map[string]*Rationale{
		"camp-intent-notes-tui": {
			SchemaVersion: SchemaVersion,
			Summary:       "Fully planned and already the primary implementation thread.",
			Confidence:    ConfidenceHigh,
		},
		"camp-artifact-commit-updates": {
			SchemaVersion: SchemaVersion,
			Summary:       "Delivered by festival CA0004; only narrow follow-up intents remain.",
			Confidence:    ConfidenceHigh,
		},
		"platform-adoption-and-extensibility": {
			SchemaVersion: SchemaVersion,
			Summary:       "Umbrella: split app compatibility, template strategy, and the ob UX audit into focused owners.",
			Confidence:    ConfidenceMedium,
		},
		"loop-scheduling-primitives": {
			SchemaVersion: SchemaVersion,
			Summary:       "Reconcile with the existing Obey executor after launch; do not build a parallel supervisor.",
			Confidence:    ConfidenceMedium,
		},
	}

	return RenderInput{
		Run:             run,
		Verdicts:        verdicts,
		Rationales:      rationales,
		EvidenceRoles:   map[EvidenceRole]int{EvidenceRoleEvidence: 7, EvidenceRoleSynthesis: 1},
		NoEvidenceCount: 1,
	}
}

// assertGolden compares rendered bytes against a checked-in golden.
func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *updateGoldens {
		require.NoError(t, os.WriteFile(path, got, 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden %s; regenerate with -update", path)
	assert.Equal(t, string(want), string(got),
		"render drifted from %s; if deliberate, regenerate with -update and read the diff", name)
}

// --- goldens -----------------------------------------------------------

// TestRenderReviewGolden pins the review document's whole shape.
func TestRenderReviewGolden(t *testing.T) {
	assertGolden(t, "golden_triage_review.md", RenderReview(renderFixture()))
}

// TestRenderPrioritiesGolden pins the priorities brief.
func TestRenderPrioritiesGolden(t *testing.T) {
	assertGolden(t, "golden_priorities.md", RenderPriorities(renderFixture()))
}

// --- the guarantees the goldens cannot express -------------------------

// TestRendersAreByteStable is the idempotence requirement: re-rendering
// unchanged data must be a no-op diff, or every review would churn its file.
func TestRendersAreByteStable(t *testing.T) {
	in := renderFixture()

	assert.Equal(t, string(RenderReview(in)), string(RenderReview(in)))
	assert.Equal(t, string(RenderPriorities(in)), string(RenderPriorities(in)))

	// A second, independently constructed fixture must render identically too,
	// which catches a renderer that accidentally depends on map iteration.
	assert.Equal(t, string(RenderReview(renderFixture())), string(RenderReview(in)))
	assert.Equal(t, string(RenderPriorities(renderFixture())), string(RenderPriorities(in)))
}

// TestRenderersReadNoClock: a "generated at" line taken from time.Now would
// make every render a diff. Every timestamp must come from the run.
func TestRenderersReadNoClock(t *testing.T) {
	in := renderFixture()

	review := string(RenderReview(in))
	priorities := string(RenderPriorities(in))

	assert.Contains(t, review, stamp(testAt), "the run's own snapshot time appears")
	for _, doc := range []string{review, priorities} {
		// 2026-08-05 is "today" for this branch; the fixture is dated 2026-08-10.
		assert.NotContains(t, doc, "2026-08-05",
			"a document must carry no timestamp except the run's own")
	}
}

// TestEveryRowAppearsExactlyOnce is the completeness requirement. A review
// that omitted a row could be approved without the operator ever seeing what
// it left out, and a row counted twice would double-count the portfolio.
func TestEveryRowAppearsExactlyOnce(t *testing.T) {
	in := renderFixture()
	lanes := BuildLanes(in.Run, in.Verdicts, in.Rationales)

	seen := map[string]int{}
	for _, lane := range lanes {
		for _, row := range lane.Rows {
			seen[row.Row.StableID]++
		}
	}

	require.Len(t, seen, len(in.Run.Manifest.Rows))
	for _, row := range in.Run.Manifest.Rows {
		assert.Equal(t, 1, seen[row.StableID], "row %s", row.StableID)
	}

	// And the rendered count table must agree with the manifest.
	review := string(RenderReview(in))
	assert.Contains(t, review, "| **Total** | **"+
		strconv.Itoa(len(in.Run.Manifest.Rows))+"** |")
}

// TestUnjudgedRowsAreSurfacedNotHidden: the fixture leaves one row unjudged,
// and both documents have to say so.
func TestUnjudgedRowsAreSurfacedNotHidden(t *testing.T) {
	in := renderFixture()

	review := string(RenderReview(in))
	priorities := string(RenderPriorities(in))

	assert.Contains(t, review, "Awaiting judgment")
	assert.Contains(t, review, "intent-tidy-the-inbox")
	assert.Contains(t, review, "no proposal yet")
	assert.Contains(t, priorities, "Not yet decided")
	assert.Contains(t, priorities, "intent-tidy-the-inbox")
}

// TestRejectedRowIsNotPresentedAsADecision: a rejected proposal is not a
// verdict to apply, so it must fall to the undecided lane rather than sit in
// the lane its dead disposition named.
func TestRejectedRowIsNotPresentedAsADecision(t *testing.T) {
	in := renderFixture()
	lanes := BuildLanes(in.Run, in.Verdicts, in.Rationales)

	undecided := laneByKey(lanes, laneUndecided)
	require.NotNil(t, undecided)
	var found bool
	for _, row := range undecided.Rows {
		if row.Row.StableID == "shared-template-sync" {
			found = true
		}
	}
	assert.True(t, found, "a rejected row awaits a new proposal")

	parked := laneByKey(lanes, laneParked)
	require.NotNil(t, parked)
	for _, row := range parked.Rows {
		assert.NotEqual(t, "shared-template-sync", row.Row.StableID)
	}
}

// TestDocumentsCarryTheGeneratedBanner is D3 in the file itself: approval by
// editing is rejected, and the document says so where a user would edit it.
func TestDocumentsCarryTheGeneratedBanner(t *testing.T) {
	in := renderFixture()

	for name, doc := range map[string]string{
		"review":     string(RenderReview(in)),
		"priorities": string(RenderPriorities(in)),
	} {
		assert.Contains(t, doc, in.Run.ID, "%s names its run", name)
		assert.Contains(t, doc, "Do not edit", "%s warns against editing", name)
		assert.Contains(t, doc, "camp triage approve", "%s names the real input path", name)
	}
}

// TestIdentityExceptionsAreCalledOut: a row identified only by path cannot be
// moved safely, so the reviewer has to see that before approving it.
func TestIdentityExceptionsAreCalledOut(t *testing.T) {
	review := string(RenderReview(renderFixture()))

	assert.Contains(t, review, "Identity exceptions")
	assert.Contains(t, review, "festival-hub-control-plane")
	assert.Contains(t, review, "path is the identity")
}

// TestRationalePipesDoNotBreakTheTable: a rationale is free text, and a pipe
// in it would silently split a markdown column.
func TestRationalePipesDoNotBreakTheTable(t *testing.T) {
	in := renderFixture()
	in.Rationales["camp-intent-notes-tui"] = &Rationale{
		SchemaVersion: SchemaVersion,
		Summary:       "shipped | verified | done",
		Confidence:    ConfidenceHigh,
	}

	review := string(RenderReview(in))

	assert.Contains(t, review, `shipped \| verified \| done`)

	var row string
	for _, line := range strings.Split(review, "\n") {
		if strings.Contains(line, "shipped") {
			row = line
		}
	}
	require.NotEmpty(t, row, "the rationale should appear in a table row")
	// Escaped pipes must not be counted as column separators, so the row still
	// has the four columns the header declares.
	assert.Equal(t, 5, strings.Count(strings.ReplaceAll(row, `\|`, ""), "|"),
		"four columns means five separators: %s", row)
}

// TestMultilineRationaleIsFlattened keeps a table row on one line.
func TestMultilineRationaleIsFlattened(t *testing.T) {
	in := renderFixture()
	in.Rationales["camp-intent-notes-tui"] = &Rationale{
		SchemaVersion: SchemaVersion,
		Summary:       "first line\nsecond line\n\nthird",
		Confidence:    ConfidenceHigh,
	}

	review := string(RenderReview(in))

	assert.Contains(t, review, "first line second line third")
}

// TestEmptyRunRendersWithoutPanicking: a run with no rows is a real state
// (an over-narrow scope), and the documents must still make sense.
func TestEmptyRunRendersWithoutPanicking(t *testing.T) {
	in := renderFixture()
	in.Run.Manifest.Rows = nil
	in.Verdicts = map[string]RowVerdict{}
	in.Rationales = map[string]*Rationale{}
	in.EvidenceRoles = map[EvidenceRole]int{}
	in.NoEvidenceCount = 0

	review := string(RenderReview(in))
	priorities := string(RenderPriorities(in))

	assert.Contains(t, review, "| **Total** | **0** |")
	assert.Contains(t, review, "Nothing in this run.")
	assert.Contains(t, review, "No evidence records stored yet.")
	assert.Contains(t, priorities, "Nothing to retire from this run.")
}

// TestSingularPhrasingReadsCorrectly: counts and verbs have to agree, or the
// document reads as machine output at exactly the moment a human is deciding
// whether to trust it.
func TestSingularPhrasingReadsCorrectly(t *testing.T) {
	in := renderFixture()
	in.Run.Manifest.Rows = in.Run.Manifest.Rows[:1]
	in.Verdicts = map[string]RowVerdict{}

	review := string(RenderReview(in))
	priorities := string(RenderPriorities(in))

	assert.Contains(t, review, "1 row has no proposal yet")
	assert.NotContains(t, review, "1 rows")
	assert.Contains(t, priorities, "1 row in this run has no proposal yet")
	assert.NotContains(t, priorities, "rows in this run has")
}
