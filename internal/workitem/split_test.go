package workitem

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var splitAt = time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

// TestParseSplitSpec covers the `<value>[:<type>]` grammar.
func TestParseSplitSpec(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    SplitSpec
		wantErr bool
	}{
		{name: "bare name inherits the parent's type", raw: "fest-ingest",
			want: SplitSpec{Value: "fest-ingest"}},
		{name: "explicit type", raw: "fest-ingest:design",
			want: SplitSpec{Value: "fest-ingest", Type: "design"}},
		{name: "a path adopts without a type", raw: "workflow/design/camp-triage",
			want: SplitSpec{Value: "workflow/design/camp-triage"}},
		{name: "a path with an explicit type splits on the LAST colon",
			raw:  "workflow/design/camp-triage:design",
			want: SplitSpec{Value: "workflow/design/camp-triage", Type: "design"}},
		{name: "surrounding space is trimmed", raw: "  fest-ingest : design  ",
			want: SplitSpec{Value: "fest-ingest", Type: "design"}},
		{name: "empty is refused", raw: "   ", wantErr: true},
		{name: "a trailing colon is refused", raw: "fest-ingest:", wantErr: true},
		{name: "a leading colon is refused", raw: ":design", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSplitSpec(tt.raw)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestParseSplitSpecsReportsEveryProblem matches how the rest of camp rejects
// input: one run, one list of what to fix.
func TestParseSplitSpecsReportsEveryProblem(t *testing.T) {
	_, err := ParseSplitSpecs([]string{"good", ":bad", "alsobad:"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), ":bad")
	assert.Contains(t, err.Error(), "alsobad:")
}

// TestValidateSplitSuccessors covers every rule checked before anything is
// written, which is the point: a duplicate in the fifth argument must not
// leave four successors on disk.
func TestValidateSplitSuccessors(t *testing.T) {
	spec := func(v string) SplitSpec { return SplitSpec{Value: v} }

	tests := []struct {
		name    string
		specs   []SplitSpec
		wantErr string
	}{
		{name: "one successor is enough", specs: []SplitSpec{spec("a")}},
		{name: "several distinct successors", specs: []SplitSpec{spec("a"), spec("b")}},
		{name: "none at all", wantErr: "at least one successor"},
		{name: "a duplicate", specs: []SplitSpec{spec("a"), spec("a")},
			wantErr: "named more than once"},
		{name: "the parent as its own successor",
			specs: []SplitSpec{spec("design-parent")}, wantErr: "cannot be its own successor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSplitSuccessors("design-parent", tt.specs)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestSplitLineageFields pins the stamps written in both directions.
func TestSplitLineageFields(t *testing.T) {
	lineage := SplitLineage{
		ParentStableID: "design-parent",
		ParentPath:     "workflow/design/parent",
		SuccessorIDs:   []string{"design-a", "design-b"},
		At:             splitAt,
	}

	parent := lineage.ParentFields()
	require.Len(t, parent, 2)
	assert.Equal(t, SplitIntoKey, parent[0].Key)
	assert.Equal(t, []string{"design-a", "design-b"}, parent[0].Values,
		"split_into is list-valued, which is why the stamper had to learn sequences")
	assert.Empty(t, parent[0].Value)
	assert.Equal(t, SplitAtKey, parent[1].Key)
	assert.Equal(t, "2026-08-05T12:00:00Z", parent[1].Value)

	// A created successor carries the seed digest too, which is what lets
	// undo prove it is untouched.
	created := lineage.SuccessorFields("sha256:abc")
	require.Len(t, created, 2)
	assert.Equal(t, SplitFromKey, created[0].Key)
	assert.Equal(t, "design-parent", created[0].Value,
		"the back-link makes the lineage readable from either end")
	assert.Equal(t, SplitSeedHashKey, created[1].Key)

	// An adopted successor had no seeded README, so it gets no seed hash —
	// and undo therefore never deletes it.
	adopted := lineage.SuccessorFields("")
	require.Len(t, adopted, 1)
	assert.Equal(t, SplitFromKey, adopted[0].Key)
}

// TestSplitLineageFieldsProduceTheRightNodeShape guards the extension made to
// the marker stamper: a list key must serialize as a sequence, not a string.
func TestSplitLineageFieldsProduceTheRightNodeShape(t *testing.T) {
	lineage := SplitLineage{SuccessorIDs: []string{"design-a"}, At: splitAt}

	into := lineage.ParentFields()[0].node()
	assert.Equal(t, "!!seq", into.Tag)
	require.Len(t, into.Content, 1)
	assert.Equal(t, "design-a", into.Content[0].Value)

	from := lineage.SuccessorFields("")[0].node()
	assert.Equal(t, "!!str", from.Tag)
}

// TestMissingSuccessors is the retirement gate's predicate.
func TestMissingSuccessors(t *testing.T) {
	tests := []struct {
		name       string
		declared   []string
		discovered map[string]bool
		want       []string
	}{
		{
			name:       "all present",
			declared:   []string{"a", "b"},
			discovered: map[string]bool{"a": true, "b": true},
		},
		{
			name:       "one missing",
			declared:   []string{"a", "b"},
			discovered: map[string]bool{"a": true},
			want:       []string{"b"},
		},
		{
			name:     "none discoverable",
			declared: []string{"b", "a"},
			want:     []string{"a", "b"},
		},
		{
			name:       "nothing declared, nothing to gate",
			discovered: map[string]bool{"a": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MissingSuccessors(tt.declared, tt.discovered))
		})
	}
}

// TestSplitGateErrorNamesTheMissing: "blocked" without the list makes the
// operator go looking for what camp already knows.
func TestSplitGateErrorNamesTheMissing(t *testing.T) {
	one := SplitGateError("design-parent", []string{"design-a"})
	assert.ErrorIs(t, one, ErrSplitGate)
	assert.Contains(t, one.Error(), "design-parent")
	assert.Contains(t, one.Error(), "design-a")
	assert.Contains(t, one.Error(), "a successor that does not exist")
	assert.Contains(t, one.Error(), "Create it:")
	assert.Contains(t, one.Error(), "--force")

	many := SplitGateError("design-parent", []string{"design-a", "design-b"})
	assert.Contains(t, many.Error(), "successors that do not exist")
	assert.Contains(t, many.Error(), "Create them:")
}

// TestSplitReadmeSeedsTheTrail covers the back-link header.
func TestSplitReadmeSeedsTheTrail(t *testing.T) {
	body := string(SplitReadme("fest-ingest", "Platform adoption", "WI-abc123", splitAt))

	assert.Contains(t, body, "# fest-ingest")
	assert.Contains(t, body, "Split from `Platform adoption` (`WI-abc123`) on 2026-08-05.")
	assert.Contains(t, body, "## Scope carried from parent")
	assert.NotContains(t, body, "Platform adoption's actual content",
		"no content is moved; the seed makes the trail and the author moves the prose")
}

// TestSplitReadmeWithoutAParentRef: legacy parents carry no ref, and the
// header still has to read as a sentence.
func TestSplitReadmeWithoutAParentRef(t *testing.T) {
	body := string(SplitReadme("fest-ingest", "Platform adoption", "", splitAt))
	assert.Contains(t, body, "Split from `Platform adoption` on 2026-08-05.")
	assert.NotContains(t, body, "()")

	bare := string(SplitReadme("fest-ingest", "", "", splitAt))
	assert.Contains(t, bare, "Split from its parent on 2026-08-05.")
	assert.NotContains(t, bare, "``")
}

// TestSplitIntoOf reads the gate's input off a marker.
func TestSplitIntoOf(t *testing.T) {
	assert.Nil(t, SplitIntoOf(nil))
	assert.Nil(t, SplitIntoOf(&Metadata{}))
	assert.Equal(t, []string{"a"}, SplitIntoOf(&Metadata{SplitInto: []string{"a"}}))
}

// TestStableIDOfResolutionOrder pins the identity order the gate joins on.
func TestStableIDOfResolutionOrder(t *testing.T) {
	assert.Equal(t, "marker-id", StableIDOf(WorkItem{
		StableID: "marker-id", SourceID: "source", Key: "design:path"}))
	assert.Equal(t, "source", StableIDOf(WorkItem{SourceID: "source", Key: "design:path"}))
	assert.Equal(t, "design:path", StableIDOf(WorkItem{Key: "design:path"}))
	assert.Equal(t, "marker-id", StableIDOf(&WorkItem{StableID: "marker-id"}))
	assert.Empty(t, StableIDOf((*WorkItem)(nil)))
}

// TestSplitMarkerFieldsAreOmitEmpty is the contract guard: a workitem that was
// never split must serialize exactly as it did before the fields existed.
func TestSplitMarkerFieldsAreOmitEmpty(t *testing.T) {
	meta := Metadata{Version: WorkitemSchemaVersion, Kind: "workitem", ID: "x", Type: "design"}
	encoded, err := marshalMetadataForTest(meta)
	require.NoError(t, err)

	for _, key := range []string{SplitIntoKey, SplitAtKey, SplitFromKey, SplitSeedHashKey} {
		assert.NotContains(t, encoded, key,
			"an unsplit workitem's marker must not gain %q", key)
	}
	assert.True(t, strings.HasPrefix(encoded, "version:"))
}

// marshalMetadataForTest encodes a marker the way camp writes one.
func marshalMetadataForTest(meta Metadata) (string, error) {
	buf, err := yaml.Marshal(&meta)
	return string(buf), err
}

// TestPristineCheck is undo's destructive-half predicate: deletion is allowed
// only on proof, and everything else is kept with a reason.
func TestPristineCheck(t *testing.T) {
	tests := []struct {
		name       string
		check      PristineCheck
		want       bool
		wantReason string
	}{
		{
			name:  "created, unedited, nothing added",
			check: PristineCheck{SeedHash: "sha256:a", ReadmeHash: "sha256:a"},
			want:  true,
		},
		{
			name:       "adopted successors are never deleted",
			check:      PristineCheck{ReadmeHash: "sha256:a"},
			wantReason: "existed before the split",
		},
		{
			name:       "an edited README keeps it",
			check:      PristineCheck{SeedHash: "sha256:a", ReadmeHash: "sha256:b"},
			wantReason: "README has been edited",
		},
		{
			name:       "a deleted README keeps it",
			check:      PristineCheck{SeedHash: "sha256:a"},
			wantReason: "README has been edited",
		},
		{
			name:       "content added beside the seed keeps it",
			check:      PristineCheck{SeedHash: "sha256:a", ReadmeHash: "sha256:a", ExtraEntries: 1},
			wantReason: "content beyond the seed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := tt.check.Pristine()
			assert.Equal(t, tt.want, got)
			if tt.wantReason == "" {
				assert.Empty(t, reason)
				return
			}
			assert.Contains(t, reason, tt.wantReason)
		})
	}
}

// TestSeedHashIsStable pins the digest undo compares against.
func TestSeedHashIsStable(t *testing.T) {
	body := SplitReadme("a", "parent", "WI-1", splitAt)
	assert.Equal(t, SeedHash(body), SeedHash(body))
	assert.NotEqual(t, SeedHash(body), SeedHash(append(body, 'x')))
	assert.True(t, strings.HasPrefix(SeedHash(body), "sha256:"))
}

// TestSplitLineageKeysCoverEveryStamp: undo removes exactly what split writes,
// so a new stamp cannot be left behind by the inverse.
func TestSplitLineageKeysCoverEveryStamp(t *testing.T) {
	lineage := SplitLineage{ParentStableID: "p", SuccessorIDs: []string{"a"}, At: splitAt}

	written := map[string]bool{}
	for _, f := range lineage.ParentFields() {
		written[f.Key] = true
	}
	for _, f := range lineage.SuccessorFields("sha256:a") {
		written[f.Key] = true
	}

	removed := map[string]bool{}
	for _, key := range SplitLineageKeys() {
		removed[key] = true
	}
	for key := range written {
		assert.True(t, removed[key], "undo must remove %q, which split writes", key)
	}
}

// TestInDungeonPathMatchesBothSpellings is a regression guard. Campaigns
// created before `camp dungeon migrate` use "dungeon"; migrated ones use the
// hidden ".dungeon". Matching only one made this answer "not dungeoned" for
// half of all campaigns, which silently defeated the retirement gate's
// dungeoned-successor rule.
func TestInDungeonPathMatchesBothSpellings(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "workflow/design/.dungeon/completed/2026-08-05/x", want: true},
		{path: "workflow/design/dungeon/completed/2026-08-05/x", want: true},
		{path: ".dungeon/archived/x", want: true},
		{path: "dungeon/archived/x", want: true},
		{path: "workflow/design/x", want: false},
		// Whole segments only: a workitem named for the concept is not in one.
		{path: "workflow/design/my-dungeon-notes", want: false},
		{path: "workflow/design/dungeoneering", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, InDungeonPath(tt.path))
		})
	}
}

// TestSplitGateErrorNamesTheFixCommands: a refusal that does not say how to
// fix it makes the operator go looking for what camp already knows.
func TestSplitGateErrorNamesTheFixCommands(t *testing.T) {
	err := SplitGateError("design-parent", []string{"design-a", "design-b"})
	message := err.Error()

	assert.Contains(t, message, "camp workitem split design-parent --into design-a")
	assert.Contains(t, message, "camp workitem split design-parent --into design-b")
	assert.Contains(t, message, "--adopt <path>", "adopting an existing home is the other fix")
	assert.Contains(t, message, "--force")
}
