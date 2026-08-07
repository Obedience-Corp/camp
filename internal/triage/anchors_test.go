package triage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnchorValidateRejects covers the union rules: an unknown tag, a missing
// field the kind needs, and a field borrowed from another kind. The last one
// matters most — a pr anchor carrying a path would look checkable to refresh
// while anchoring nothing.
func TestAnchorValidateRejects(t *testing.T) {
	tests := []struct {
		name       string
		anchor     Anchor
		wantFields []string
	}{
		{
			name:       "unknown kind",
			anchor:     Anchor{Kind: "issue", ID: "42", Observed: "open"},
			wantFields: []string{"anchor.kind"},
		},
		{
			name:       "missing kind",
			anchor:     Anchor{Observed: "merged"},
			wantFields: []string{"anchor.kind"},
		},
		{
			name:       "pr without repo, number, or observed",
			anchor:     Anchor{Kind: AnchorKindPR},
			wantFields: []string{"anchor.repo", "anchor.number", "anchor.observed"},
		},
		{
			name:       "pr with a path borrowed from another kind",
			anchor:     Anchor{Kind: AnchorKindPR, Repo: "o/r", Number: 1, Observed: "open", Path: "x"},
			wantFields: []string{"anchor.path"},
		},
		{
			name:       "pr repo missing its owner",
			anchor:     Anchor{Kind: AnchorKindPR, Repo: "obey", Number: 1, Observed: "open"},
			wantFields: []string{"anchor.repo"},
		},
		{
			name:       "path without a hash",
			anchor:     Anchor{Kind: AnchorKindPath, Path: "projects/camp"},
			wantFields: []string{"anchor.hash"},
		},
		{
			name:       "path hash without its algorithm",
			anchor:     Anchor{Kind: AnchorKindPath, Path: "projects/camp", Hash: "deadbeef"},
			wantFields: []string{"anchor.hash"},
		},
		{
			name:       "absolute path escapes the campaign",
			anchor:     Anchor{Kind: AnchorKindPath, Path: "/etc/passwd", Hash: PathHashPrefix + "a"},
			wantFields: []string{"anchor.path"},
		},
		{
			name:       "parent traversal escapes the campaign",
			anchor:     Anchor{Kind: AnchorKindPath, Path: "../outside", Hash: PathHashPrefix + "a"},
			wantFields: []string{"anchor.path"},
		},
		{
			name:       "festival without an observed value",
			anchor:     Anchor{Kind: AnchorKindFestival, ID: "CI0009"},
			wantFields: []string{"anchor.observed"},
		},
		{
			name:       "workitem using observed instead of observed_stage",
			anchor:     Anchor{Kind: AnchorKindWorkitem, StableID: "x", Observed: "active"},
			wantFields: []string{"anchor.observed", "anchor.observed_stage"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			violations := tc.anchor.Validate()

			require.NotEmpty(t, violations)
			for _, want := range tc.wantFields {
				assert.Contains(t, fieldsOf(violations), want)
			}
		})
	}
}

// TestAnchorValidateAccepts covers one well-formed anchor of every kind, plus
// the offline case: an anchor refresh could not check records that it could
// not check, and stays valid.
func TestAnchorValidateAccepts(t *testing.T) {
	tests := []struct {
		name   string
		anchor Anchor
	}{
		{
			name:   "pr with a head sha",
			anchor: Anchor{Kind: AnchorKindPR, Repo: "Obedience-Corp/obey", Number: 239, Observed: "merged", SHA: "abc"},
		},
		{
			name:   "pr without a head sha",
			anchor: Anchor{Kind: AnchorKindPR, Repo: "Obedience-Corp/obey", Number: 239, Observed: "open"},
		},
		{
			name:   "pr that could not be checked offline",
			anchor: Anchor{Kind: AnchorKindPR, Repo: "Obedience-Corp/obey", Number: 239, Observed: ObservedUncheckedOffline},
		},
		{
			name:   "path",
			anchor: Anchor{Kind: AnchorKindPath, Path: "projects/camp/internal/triage", Hash: PathHashPrefix + "deadbeef"},
		},
		{
			name:   "festival",
			anchor: Anchor{Kind: AnchorKindFestival, ID: "CI0009", Observed: "active"},
		},
		{
			name:   "workitem",
			anchor: Anchor{Kind: AnchorKindWorkitem, StableID: "design-thing", ObservedStage: "active"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Empty(t, tc.anchor.Validate())
		})
	}
}

// TestAnchorViolationsAreNestedByIndex proves a bad anchor is reported at its
// position in the record rather than as an anonymous "anchors" failure.
func TestAnchorViolationsAreNestedByIndex(t *testing.T) {
	record := validEvidence()
	record.Anchors[2].Observed = ""

	assert.Contains(t, fieldsOf(record.Validate()), "anchors[2].observed")
}
