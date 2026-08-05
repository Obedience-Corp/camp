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

	successor := lineage.SuccessorFields()
	require.Len(t, successor, 1)
	assert.Equal(t, SplitFromKey, successor[0].Key)
	assert.Equal(t, "design-parent", successor[0].Value,
		"the back-link makes the lineage readable from either end")
}

// TestSplitLineageFieldsProduceTheRightNodeShape guards the extension made to
// the marker stamper: a list key must serialize as a sequence, not a string.
func TestSplitLineageFieldsProduceTheRightNodeShape(t *testing.T) {
	lineage := SplitLineage{SuccessorIDs: []string{"design-a"}, At: splitAt}

	into := lineage.ParentFields()[0].node()
	assert.Equal(t, "!!seq", into.Tag)
	require.Len(t, into.Content, 1)
	assert.Equal(t, "design-a", into.Content[0].Value)

	from := lineage.SuccessorFields()[0].node()
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
	assert.Contains(t, one.Error(), "Create it,")
	assert.Contains(t, one.Error(), "--force")

	many := SplitGateError("design-parent", []string{"design-a", "design-b"})
	assert.Contains(t, many.Error(), "successors that do not exist")
	assert.Contains(t, many.Error(), "Create them,")
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

	for _, key := range []string{SplitIntoKey, SplitAtKey, SplitFromKey} {
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
