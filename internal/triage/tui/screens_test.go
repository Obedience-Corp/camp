package tui

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/triage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// screenFixture builds a run with one row in each of several lanes.
func screenFixture() (*triage.Run, *triage.Status, []triage.Lane) {
	manifest := &triage.Manifest{
		SchemaVersion: triage.SchemaVersion,
		RunID:         "run-20260810T140000Z",
		Mode:          triage.RunModeFull,
		Profile: triage.ManifestProfile{
			Name: triage.ProfileNameDefault, Resolved: triage.DefaultProfile(),
		},
		CreatedAt: testAt,
	}
	specs := []struct{ id, title string }{
		{"design-current", "The current thread"},
		{"design-parked", "Parked for later"},
		{"design-done", "Delivered already"},
		{"design-split", "An umbrella"},
	}
	for _, spec := range specs {
		manifest.Rows = append(manifest.Rows, triage.ManifestRow{
			StableID: spec.id, Key: "design:" + spec.id, Type: "design",
			Title: spec.title, RelativePath: "workflow/design/" + spec.id,
			Ref: "WI-abc123", LifecycleStage: "active", Batch: 1,
			Policy: triage.RowPolicy{
				Evidence: triage.EvidenceDepthDeep, RoutingTier: triage.RoutingTierDefault,
			},
		})
	}
	manifest.Normalize()

	run := &triage.Run{
		ID: manifest.RunID, Dir: "/campaign/run", Manifest: manifest,
		State: &triage.RunState{
			SchemaVersion: triage.SchemaVersion, RunID: manifest.RunID,
			Phase:        triage.PhaseReviewing,
			PhaseHistory: []triage.PhaseTransition{{Phase: triage.PhaseCreated, At: testAt}},
		},
	}

	proposed := func(disposition string, action triage.CanonicalAction) triage.RowVerdict {
		return triage.RowVerdict{
			State: triage.VerdictProposed, Disposition: disposition,
			CanonicalAction: action, Actor: "tester", At: testAt,
		}
	}
	verdicts := map[string]triage.RowVerdict{
		"design-current": proposed("current", "attention/current"),
		"design-parked":  proposed("parked", "attention/parked"),
		"design-done":    proposed("completed", "dungeon/completed"),
		"design-split":   proposed("consolidate", triage.ActionSplit),
	}
	rationales := map[string]*triage.Rationale{
		"design-current": {
			SchemaVersion: triage.SchemaVersion,
			Summary:       "the primary thread", Confidence: triage.ConfidenceHigh,
		},
	}
	lanes := triage.BuildLanes(run, verdicts, rationales)
	return run, triage.StatusFrom(run, verdicts), lanes
}

// --- screen 1 ----------------------------------------------------------

// TestScreen1ShowsTheLaneTableAndMarksTerminalLanes: the reader has to see
// which lanes will confirm per row before choosing where to start.
func TestScreen1ShowsTheLaneTableAndMarksTerminalLanes(t *testing.T) {
	run, status, lanes := screenFixture()

	screen := Screen1(run, status, lanes)

	assert.Contains(t, screen, "TRIAGE run-20260810T140000Z")
	assert.Contains(t, screen, "Profile: default")
	// The lane column sizes to the widest name present, so the header and
	// every row must agree on where the count column starts. A pty run caught
	// this misaligning when a lane name reached the fixed width.
	lines := strings.Split(screen, "\n")
	var header string
	for _, line := range lines {
		if strings.Contains(line, "Lane") && strings.Contains(line, "rows") {
			header = line
		}
	}
	require.NotEmpty(t, header, "the table has a header")
	countColumn := strings.Index(header, "rows")
	for _, lane := range lanes {
		name := triage.LaneSelectorName(lane.Title)
		prefix := "  " + name
		for _, line := range lines {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			rest := line[len(prefix):]
			offset := len(rest) - len(strings.TrimLeft(rest, " "))
			assert.Equal(t, countColumn, len(prefix)+offset,
				"lane %s starts its count at the header column: %q", name, line)
		}
	}

	for _, line := range strings.Split(screen, "\n") {
		switch {
		case strings.Contains(line, "close-as-delivered"),
			strings.Contains(line, "consolidate-and-retire"):
			assert.Contains(t, line, "terminal, confirms per row",
				"a terminal lane is marked: %s", line)
		case strings.Contains(line, "  park-for-later"):
			assert.NotContains(t, line, "terminal")
		}
	}
}

// TestScreen1SurfacesStalenessRatherThanBuryingIt.
func TestScreen1SurfacesStalenessRatherThanBuryingIt(t *testing.T) {
	run, status, lanes := screenFixture()

	screen := Screen1(run, status, lanes)

	assert.Contains(t, screen, AnchorUnchecked,
		"anchors render unchecked until refresh measures them, never as fresh")
	assert.NotContains(t, screen, AnchorFresh,
		"nothing claims a freshness nobody measured")
}

// TestScreen1LaneNamesAreTheSelectorTokens: what a user reads is what they can
// type into `camp triage approve --lane`.
func TestScreen1LaneNamesAreTheSelectorTokens(t *testing.T) {
	run, status, lanes := screenFixture()

	screen := Screen1(run, status, lanes)

	for _, lane := range lanes {
		assert.Contains(t, screen, triage.LaneSelectorName(lane.Title))
	}
}

// TestScreen1HandlesAnEmptyRun without panicking or lying.
func TestScreen1HandlesAnEmptyRun(t *testing.T) {
	run, status, _ := screenFixture()
	run.Manifest.Rows = nil
	status.Rows = 0

	screen := Screen1(run, status, nil)

	assert.Contains(t, screen, "(no rows in this run)")
}

// --- screen 2 ----------------------------------------------------------

// TestScreen2ListsOneLinePerRow with the fields the spec names.
func TestScreen2ListsOneLinePerRow(t *testing.T) {
	_, _, lanes := screenFixture()
	lane := findLane(lanes, "Current")
	require.NotNil(t, lane)

	screen := Screen2(*lane, 0)

	assert.Contains(t, screen, "CURRENT")
	assert.Contains(t, screen, "The current thread")
	assert.Contains(t, screen, "design")
	assert.Contains(t, screen, "current")
	assert.Contains(t, screen, "high", "confidence comes from the rationale")
	assert.Contains(t, screen, AnchorUnchecked)
}

// TestScreen2PagesByBatch so a full run reviews in sittings.
func TestScreen2PagesByBatch(t *testing.T) {
	_, _, lanes := screenFixture()
	lane := findLane(lanes, "Current")
	require.NotNil(t, lane)

	assert.Contains(t, Screen2(*lane, 1), "batch 1")
	assert.Contains(t, Screen2(*lane, 9), "(no rows)",
		"a batch with nothing in it says so rather than showing everything")
}

// --- screen 3 ----------------------------------------------------------

// TestScreen3ShowsTheEvidenceTrioAndTheRealCommand: the reader approves an
// action, not a label.
func TestScreen3ShowsTheEvidenceTrioAndTheRealCommand(t *testing.T) {
	_, _, lanes := screenFixture()
	lane := findLane(lanes, "Close as delivered")
	require.NotNil(t, lane)
	row := lane.Rows[0]

	screen := Screen3(CardInput{
		Row: row, Position: 1, Total: 1, Lane: "close-as-delivered",
		Evidence: &triage.EvidenceRecord{
			SchemaVersion: triage.SchemaVersion, StableID: row.Row.StableID,
			OriginalGoal:  "ship it",
			Delivered:     []string{"core schema", "filters"},
			Missing:       []string{"doctor rename repair"},
			OpenDecisions: []string{"captured as an intent"},
			Confidence:    triage.ConfidenceHigh,
			ProducedBy: triage.ProducedBy{
				Role: triage.EvidenceRoleEvidence, Runtime: "fixture", At: testAt,
			},
		},
	})

	assert.Contains(t, screen, "[close-as-delivered 1/1]")
	assert.Contains(t, screen, "Delivered: core schema · filters")
	assert.Contains(t, screen, "Missing:   doctor rename repair")
	assert.Contains(t, screen, "Follow-up: captured as an intent")
	assert.Contains(t, screen, "Will run:  camp workitem promote design-done --target completed")
	assert.Contains(t, screen, "terminal and confirms individually")
}

// TestScreen3CompactVariantDropsTheEvidenceBlock so a sweep pass moves at
// seconds per row.
func TestScreen3CompactVariantDropsTheEvidenceBlock(t *testing.T) {
	_, _, lanes := screenFixture()
	lane := findLane(lanes, "Park for later")
	require.NotNil(t, lane)

	screen := Screen3(CardInput{
		Row: lane.Rows[0], Position: 1, Total: 1, Lane: "park-for-later",
		Evidence: &triage.EvidenceRecord{
			SchemaVersion: triage.SchemaVersion, StableID: lane.Rows[0].Row.StableID,
			Delivered: []string{"should not appear"},
		},
		Compact: true,
	})

	assert.NotContains(t, screen, "should not appear")
	assert.NotContains(t, screen, "Delivered:")
	assert.Contains(t, screen, "Will run:", "the action still shows")
}

// TestScreen3ListsSuccessorsWithTheirState is doc 06's queue inline: a parent
// is not retired until every successor exists, so the card has to show which
// are missing.
func TestScreen3ListsSuccessorsWithTheirState(t *testing.T) {
	_, _, lanes := screenFixture()
	lane := findLane(lanes, "Consolidate and retire")
	require.NotNil(t, lane)

	screen := Screen3(CardInput{
		Row: lane.Rows[0], Position: 1, Total: 1, Lane: "consolidate-and-retire",
		Successors: []Successor{
			{StableID: "successor-present", Exists: true},
			{StableID: "successor-absent", Exists: false},
		},
	})

	assert.Contains(t, screen, "Successors:")
	assert.Contains(t, screen, "successor-present")
	assert.Contains(t, screen, "exists")
	assert.Contains(t, screen, "successor-absent")
	assert.Contains(t, screen, "missing")
}

// TestScreen3WithoutEvidenceSaysSo rather than rendering a blank trio.
func TestScreen3WithoutEvidenceSaysSo(t *testing.T) {
	_, _, lanes := screenFixture()
	lane := findLane(lanes, "Current")
	require.NotNil(t, lane)

	screen := Screen3(CardInput{Row: lane.Rows[0], Position: 1, Total: 1, Lane: "current"})

	assert.Contains(t, screen, "(no evidence record)")
}

// TestScreen3NoEvidenceMarkerReadsHonestly.
func TestScreen3NoEvidenceMarkerReadsHonestly(t *testing.T) {
	_, _, lanes := screenFixture()
	lane := findLane(lanes, "Current")
	require.NotNil(t, lane)

	screen := Screen3(CardInput{
		Row: lane.Rows[0], Position: 1, Total: 1, Lane: "current",
		Evidence: &triage.EvidenceRecord{
			SchemaVersion: triage.SchemaVersion,
			StableID:      lane.Rows[0].Row.StableID,
			NoEvidence:    true,
			ProducedBy: triage.ProducedBy{
				Role: triage.EvidenceRoleHuman, Runtime: "human", At: testAt,
			},
		},
	})

	assert.Contains(t, screen, "Judged without a gathered record")
}

// TestScreensTruncateRatherThanWrap keeps a card line a line.
func TestScreensTruncateRatherThanWrap(t *testing.T) {
	_, _, lanes := screenFixture()
	lane := findLane(lanes, "Current")
	require.NotNil(t, lane)
	row := lane.Rows[0]
	row.Row.Title = strings.Repeat("very long title ", 20)

	line := RowLine(row)

	assert.NotContains(t, line, "\n")
	assert.Less(t, len([]rune(line)), 120, "the line stays inside a normal terminal")
}
