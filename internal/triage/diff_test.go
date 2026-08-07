package triage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// diffRow is the manifest row every classifier case starts from.
func diffRow() ManifestRow {
	return ManifestRow{
		StableID:       "design-observation-boundary",
		Ref:            "WI-a1b2c3",
		Key:            "design:workflow/design/observation-boundary",
		Type:           "design",
		Title:          "Observation boundary",
		RelativePath:   "workflow/design/observation-boundary",
		LifecycleStage: "active",
		AttentionStage: "parked",
		Batch:          1,
	}
}

// discoveredFrom is the row re-discovered exactly where it was, which is the
// "nothing moved" baseline every case perturbs one field of.
func discoveredFrom(row ManifestRow) DiscoveredItem {
	return DiscoveredItem{
		StableID:       row.StableID,
		Key:            row.Key,
		Type:           row.Type,
		Title:          row.Title,
		RelativePath:   row.RelativePath,
		LifecycleStage: row.LifecycleStage,
		AttentionStage: row.AttentionStage,
	}
}

func pathAnchor(path, hash string) Anchor {
	return Anchor{Kind: AnchorKindPath, Path: path, Hash: PathHashPrefix + hash}
}

// TestClassifyRows covers every class and the precedence between them.
func TestClassifyRows(t *testing.T) {
	tests := []struct {
		name string
		// mutate perturbs the baseline: the row, its re-discovery (nil to
		// delete it), and its anchor checks.
		row        func(*ManifestRow)
		discovered func(*DiscoveredItem) bool
		checks     []AnchorCheck
		wantClass  RowClass
		wantReason []string
	}{
		{
			name:       "gone when the id is not discoverable",
			discovered: func(*DiscoveredItem) bool { return false },
			wantClass:  ClassGone,
			wantReason: []string{"no longer discoverable outside dungeons",
				"workflow/design/observation-boundary", "external completion"},
		},
		{
			name: "gone outranks a changed anchor",
			// A row that both vanished and whose anchor moved is gone: the
			// anchor result describes an item that is no longer there.
			discovered: func(*DiscoveredItem) bool { return false },
			checks: []AnchorCheck{{
				Anchor:   pathAnchor("docs/x.md", "aaa"),
				Observed: PathHashPrefix + "bbb",
			}},
			wantClass:  ClassGone,
			wantReason: []string{"no longer discoverable"},
		},
		{
			name: "changed when a path anchor's hash moved",
			checks: []AnchorCheck{{
				Anchor:   pathAnchor("docs/x.md", "aaa"),
				Observed: PathHashPrefix + "bbb",
			}},
			wantClass: ClassChanged,
			wantReason: []string{"anchor changed", "path:docs/x.md",
				"sha256:aaa", "sha256:bbb"},
		},
		{
			name: "changed names every mismatching anchor, not just the first",
			checks: []AnchorCheck{
				{Anchor: pathAnchor("docs/x.md", "aaa"), Observed: PathHashPrefix + "bbb"},
				{
					Anchor:   Anchor{Kind: AnchorKindFestival, ID: "CI0009", Observed: "active"},
					Observed: "completed",
				},
			},
			wantClass: ClassChanged,
			wantReason: []string{"anchors changed", "path:docs/x.md",
				"festival:CI0009", "active", "completed"},
		},
		{
			name: "changed when a deleted anchor file observes nothing",
			checks: []AnchorCheck{{
				Anchor: pathAnchor("docs/gone.md", "aaa"), Observed: "",
			}},
			wantClass:  ClassChanged,
			wantReason: []string{"path:docs/gone.md", `""`},
		},
		{
			name: "changed outranks moved so a stale verdict cannot apply",
			// The safety case: this row both moved AND lost its anchor. If
			// precedence ran the other way it would classify moved, keep its
			// verdict, and apply against evidence that no longer holds.
			row: func(r *ManifestRow) { r.RelativePath = "workflow/design/old" },
			checks: []AnchorCheck{{
				Anchor:   pathAnchor("docs/x.md", "aaa"),
				Observed: PathHashPrefix + "bbb",
			}},
			wantClass:  ClassChanged,
			wantReason: []string{"anchor changed"},
		},
		{
			name:      "moved when the path changed",
			row:       func(r *ManifestRow) { r.RelativePath = "workflow/design/old-home" },
			wantClass: ClassMoved,
			wantReason: []string{"identity resolved at a new location", "path",
				"workflow/design/old-home", "workflow/design/observation-boundary",
				"verdict stands"},
		},
		{
			name:       "moved when only the lifecycle stage advanced",
			row:        func(r *ManifestRow) { r.LifecycleStage = "planning" },
			wantClass:  ClassMoved,
			wantReason: []string{"lifecycle", "planning", "active"},
		},
		{
			name:       "moved when only the attention stage advanced",
			row:        func(r *ManifestRow) { r.AttentionStage = "active" },
			wantClass:  ClassMoved,
			wantReason: []string{"attention", "active", "parked"},
		},
		{
			name:       "moved reports only the dimension that differs",
			row:        func(r *ManifestRow) { r.AttentionStage = "active" },
			wantClass:  ClassMoved,
			wantReason: []string{"attention"},
		},
		{
			name: "a path-bound row that moved is changed, not moved",
			// FT-008: an identity exception is bound to a path, so the move
			// invalidates the only identity the row has. It is not gone —
			// the item is right there — so saying so would send a human
			// looking for a completion that never happened.
			row: func(r *ManifestRow) {
				r.RelativePath = "workflow/design/old-home"
				r.IdentityException = &IdentityException{
					Path: "workflow/design/old-home", Reason: "no marker",
				}
			},
			wantClass: ClassChanged,
			wantReason: []string{"identity exception was bound to",
				"workflow/design/old-home", "adopt it"},
		},
		{
			name: "a path-bound row that did not move stays fresh",
			row: func(r *ManifestRow) {
				r.IdentityException = &IdentityException{
					Path: "workflow/design/observation-boundary", Reason: "no marker",
				}
			},
			wantClass:  ClassFresh,
			wantReason: []string{"identity resolves at the same location"},
		},
		{
			name:       "fresh with no anchors says so rather than claiming a check",
			wantClass:  ClassFresh,
			wantReason: []string{"no anchors to check"},
		},
		{
			name: "fresh when every anchor matches",
			checks: []AnchorCheck{{
				Anchor:   pathAnchor("docs/x.md", "aaa"),
				Observed: PathHashPrefix + "aaa",
			}},
			wantClass:  ClassFresh,
			wantReason: []string{"all 1 anchor(s) match"},
		},
		{
			name: "an unchecked anchor is not a change",
			// The offline case. Treating unchecked as a mismatch would make
			// every offline refresh invalidate the whole run.
			checks: []AnchorCheck{{
				Anchor:    Anchor{Kind: AnchorKindPR, Repo: "o/c", Number: 1, Observed: "open"},
				Observed:  ObservedUncheckedOffline,
				Unchecked: true,
			}},
			wantClass:  ClassFresh,
			wantReason: []string{"all 1 anchor(s) unchecked"},
		},
		{
			name: "a partially checked row reports both counts",
			checks: []AnchorCheck{
				{Anchor: pathAnchor("docs/x.md", "aaa"), Observed: PathHashPrefix + "aaa"},
				{
					Anchor:    Anchor{Kind: AnchorKindPR, Repo: "o/c", Number: 1, Observed: "open"},
					Observed:  ObservedUncheckedOffline,
					Unchecked: true,
				},
			},
			wantClass:  ClassFresh,
			wantReason: []string{"1 of 2 anchor(s) match", "1 unchecked"},
		},
		{
			name: "a real mismatch still wins over an unchecked sibling",
			checks: []AnchorCheck{
				{
					Anchor:    Anchor{Kind: AnchorKindPR, Repo: "o/c", Number: 1, Observed: "open"},
					Observed:  ObservedUncheckedOffline,
					Unchecked: true,
				},
				{Anchor: pathAnchor("docs/x.md", "aaa"), Observed: PathHashPrefix + "bbb"},
			},
			wantClass:  ClassChanged,
			wantReason: []string{"path:docs/x.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := diffRow()
			if tt.row != nil {
				tt.row(&row)
			}
			item := discoveredFrom(diffRow())
			discovered := map[string]DiscoveredItem{item.StableID: item}
			if tt.discovered != nil && !tt.discovered(&item) {
				discovered = map[string]DiscoveredItem{}
			}

			diff := ClassifyRows(DiffInput{
				Rows:       []ManifestRow{row},
				Discovered: discovered,
				Anchors:    map[string][]AnchorCheck{row.StableID: tt.checks},
			})

			require.Len(t, diff.Rows, 1)
			got := diff.Rows[0]
			assert.Equal(t, tt.wantClass, got.Class)
			for _, want := range tt.wantReason {
				assert.Contains(t, got.Reason, want,
					"reason should explain the class")
			}
		})
	}
}

// TestClassifyRowsFindsNewDiscoveries covers the fifth class, which comes from
// the discovery side rather than the manifest side.
func TestClassifyRowsFindsNewDiscoveries(t *testing.T) {
	row := diffRow()
	known := discoveredFrom(row)
	fresh := DiscoveredItem{
		StableID:     "design-brand-new",
		Key:          "design:workflow/design/brand-new",
		Type:         "design",
		RelativePath: "workflow/design/brand-new",
	}

	diff := ClassifyRows(DiffInput{
		Rows: []ManifestRow{row},
		Discovered: map[string]DiscoveredItem{
			known.StableID: known,
			fresh.StableID: fresh,
		},
	})

	require.Len(t, diff.Rows, 2)
	assert.Equal(t, ClassFresh, diff.Rows[0].Class)

	got := diff.Rows[1]
	assert.Equal(t, ClassNew, got.Class)
	assert.Equal(t, "design-brand-new", got.StableID)
	assert.Contains(t, got.Reason, "absent from the run snapshot")
	require.NotNil(t, got.Discovered,
		"a new row must carry the item so the effects layer can build a manifest row")
	assert.Equal(t, "workflow/design/brand-new", got.Discovered.RelativePath)
}

// TestClassifyRowsRequiresTheWholeManifest is the trap the DiffInput doc
// warns about: passing only the decided rows would classify every undecided
// row as a new discovery and duplicate it into the manifest.
func TestClassifyRowsRequiresTheWholeManifest(t *testing.T) {
	decided := diffRow()
	undecided := diffRow()
	undecided.StableID = "design-not-yet-judged"
	undecided.Key = "design:workflow/design/not-yet-judged"
	undecided.RelativePath = "workflow/design/not-yet-judged"

	discovered := map[string]DiscoveredItem{
		decided.StableID:   discoveredFrom(decided),
		undecided.StableID: discoveredFrom(undecided),
	}

	whole := ClassifyRows(DiffInput{
		Rows: []ManifestRow{decided, undecided}, Discovered: discovered,
	})
	assert.Equal(t, 0, whole.CountByClass()[ClassNew],
		"a row already in the manifest is never new")

	partial := ClassifyRows(DiffInput{
		Rows: []ManifestRow{decided}, Discovered: discovered,
	})
	assert.Equal(t, 1, partial.CountByClass()[ClassNew],
		"this is why Rows must be the whole manifest, not the decided subset")
}

// TestClassifyRowsIsDeterministic pins the ordering guarantee two refreshes
// over an unchanged campaign depend on.
func TestClassifyRowsIsDeterministic(t *testing.T) {
	row := diffRow()
	discovered := map[string]DiscoveredItem{row.StableID: discoveredFrom(row)}
	for _, id := range []string{"z-item", "a-item", "m-item"} {
		discovered[id] = DiscoveredItem{
			StableID: id, Key: "design:workflow/design/" + id,
			RelativePath: "workflow/design/" + id,
		}
	}

	first := ClassifyRows(DiffInput{Rows: []ManifestRow{row}, Discovered: discovered})
	for range 5 {
		again := ClassifyRows(DiffInput{Rows: []ManifestRow{row}, Discovered: discovered})
		assert.Equal(t, first.Rows, again.Rows,
			"map iteration order must not reach the output")
	}

	ids := make([]string, 0, len(first.Rows))
	for _, r := range first.InClass(ClassNew) {
		ids = append(ids, r.StableID)
	}
	assert.Equal(t, []string{"a-item", "m-item", "z-item"}, ids,
		"new rows sort by key")
}

// TestClassifyRowsIsPure guards the reuse rule from spec doc 01: the
// classifier takes snapshots and must not mutate them.
func TestClassifyRowsIsPure(t *testing.T) {
	row := diffRow()
	row.RelativePath = "workflow/design/old-home"
	item := discoveredFrom(diffRow())
	before := row

	ClassifyRows(DiffInput{
		Rows:       []ManifestRow{row},
		Discovered: map[string]DiscoveredItem{item.StableID: item},
	})

	assert.Equal(t, before, row,
		"classification must not re-key the caller's row; the effects layer does that")
}

// TestRowClassApplicable pins which classes apply may execute. Getting this
// wrong is how a stale verdict reaches a real directory move.
func TestRowClassApplicable(t *testing.T) {
	applicable := map[RowClass]bool{
		ClassFresh: true, ClassMoved: true,
		ClassChanged: false, ClassGone: false, ClassNew: false,
	}
	for _, name := range RowClasses() {
		class := RowClass(name)
		assert.Equal(t, applicable[class], class.Applicable(), "class %q", name)
	}
}

// TestDiffCounters covers the reporting the refresh output is built from.
func TestDiffCounters(t *testing.T) {
	gone := diffRow()
	unchecked := diffRow()
	unchecked.StableID = "design-with-unchecked"
	unchecked.Key = "design:workflow/design/unchecked"
	unchecked.RelativePath = "workflow/design/unchecked"

	diff := ClassifyRows(DiffInput{
		Rows: []ManifestRow{gone, unchecked},
		Discovered: map[string]DiscoveredItem{
			unchecked.StableID: discoveredFrom(unchecked),
		},
		Anchors: map[string][]AnchorCheck{
			unchecked.StableID: {{
				Anchor:    Anchor{Kind: AnchorKindPR, Repo: "o/c", Number: 1, Observed: "open"},
				Observed:  ObservedUncheckedOffline,
				Unchecked: true,
			}},
		},
	})

	counts := diff.CountByClass()
	assert.Equal(t, 1, counts[ClassGone])
	assert.Equal(t, 1, counts[ClassFresh])
	assert.Equal(t, 1, diff.RowsWithUncheckedAnchors(),
		"an unchecked anchor is reported even on a fresh row")
	require.Len(t, diff.InClass(ClassGone), 1)
	assert.Equal(t, gone.StableID, diff.InClass(ClassGone)[0].StableID)
}

// TestFT006CI0009DriftReplay is the sequence's Done When condition, replayed
// from the field trial: CI0009 was promoted mid-run and then activated. The
// row must classify moved with its verdict intact, and once the manifest is
// re-keyed it must classify fresh — not gone, and not changed.
func TestFT006CI0009DriftReplay(t *testing.T) {
	// As snapshotted: an intent sitting in the inbox.
	row := ManifestRow{
		StableID:       "CI0009",
		Ref:            "WI-c10009",
		Key:            "intent:.campaign/intents/inbox/ci0009-meeting-keys.md",
		Type:           "intent",
		Title:          "Meeting keys",
		RelativePath:   ".campaign/intents/inbox/ci0009-meeting-keys.md",
		LifecycleStage: "inbox",
		AttentionStage: "",
		Batch:          1,
	}
	// As re-discovered: promoted to ready, then activated.
	drifted := DiscoveredItem{
		StableID:       "CI0009",
		Key:            "intent:.campaign/intents/ready/ci0009-meeting-keys.md",
		Type:           "intent",
		Title:          "Meeting keys",
		RelativePath:   ".campaign/intents/ready/ci0009-meeting-keys.md",
		LifecycleStage: "ready",
		AttentionStage: "active",
	}

	first := ClassifyRows(DiffInput{
		Rows:       []ManifestRow{row},
		Discovered: map[string]DiscoveredItem{"CI0009": drifted},
	})
	require.Len(t, first.Rows, 1)
	got := first.Rows[0]

	assert.Equal(t, ClassMoved, got.Class,
		"a promoted-then-activated row moved; identity survives moves (FT-006)")
	assert.Contains(t, got.Reason, "verdict stands")
	assert.True(t, got.Class.Applicable(),
		"the verdict must survive the drift, which is the whole point of FT-006")
	require.NotNil(t, got.Moved)
	assert.Equal(t, ".campaign/intents/inbox/ci0009-meeting-keys.md", got.Moved.FromRelativePath)
	assert.Equal(t, ".campaign/intents/ready/ci0009-meeting-keys.md", got.Moved.ToRelativePath)
	assert.Equal(t, "inbox", got.Moved.FromLifecycleStage)
	assert.Equal(t, "ready", got.Moved.ToLifecycleStage)
	assert.Equal(t, "active", got.Moved.ToAttentionStage)
	// Both dimensions are named, so a human reading the refresh sees the
	// whole drift rather than only the half that happened to be checked first.
	assert.True(t, strings.Contains(got.Reason, "lifecycle") &&
		strings.Contains(got.Reason, "attention"),
		"reason should name both the promotion and the activation, got %q", got.Reason)

	// Apply the re-key the effects layer would perform, then re-classify.
	rekeyRow(&row, *got.Moved)
	second := ClassifyRows(DiffInput{
		Rows:       []ManifestRow{row},
		Discovered: map[string]DiscoveredItem{"CI0009": drifted},
	})
	require.Len(t, second.Rows, 1)
	assert.Equal(t, ClassFresh, second.Rows[0].Class,
		"moved -> re-key -> fresh; a second refresh must not re-report the same move")
}
