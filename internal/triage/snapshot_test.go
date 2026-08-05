package triage

import (
	"strconv"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// item builds a discovered workitem fixture.
func item(id, wfType, key, stage, attention string) workitem.WorkItem {
	return workitem.WorkItem{
		Key:            key,
		WorkflowType:   workitem.WorkflowType(wfType),
		LifecycleStage: workitem.LifecycleStage(stage),
		AttentionStage: attention,
		Title:          "Title for " + key,
		RelativePath:   "workflow/" + wfType + "/" + key,
		StableID:       id,
		SourceID:       id,
		SourceMetadata: map[string]any{"ref": workitem.Derive(id)},
	}
}

func snapshotInput(items []workitem.WorkItem) SnapshotInput {
	return SnapshotInput{
		ProfileName: ProfileNameDefault,
		Profile:     DefaultProfile(),
		Mode:        RunModeFull,
		Items:       items,
		Now:         testAt,
	}
}

// --- error cases first -------------------------------------------------

// TestBuildManifestRejectsUnsnapshotableItems: a snapshot that cannot produce
// a valid manifest must fail as a snapshot problem naming the row, not as an
// opaque store write failure later.
func TestBuildManifestRejectsUnsnapshotableItems(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*workitem.WorkItem)
		wantField string
	}{
		{
			name:      "no discovery key",
			mutate:    func(w *workitem.WorkItem) { w.Key = "" },
			wantField: "rows[0].key",
		},
		{
			name:      "no type",
			mutate:    func(w *workitem.WorkItem) { w.WorkflowType = "" },
			wantField: "rows[0].type",
		},
		{
			name:      "no path",
			mutate:    func(w *workitem.WorkItem) { w.RelativePath = "" },
			wantField: "rows[0].relative_path",
		},
		{
			name:      "unknown attention stage",
			mutate:    func(w *workitem.WorkItem) { w.AttentionStage = "urgent" },
			wantField: "rows[0].attention_stage",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := item("design-a", "design", "design:a", "active", "active")
			tc.mutate(&w)

			_, err := BuildManifest(snapshotInput([]workitem.WorkItem{w}))

			require.Error(t, err)
			require.ErrorIs(t, err, camperrors.ErrInvalidInput)
			assert.Contains(t, violatedFields(err), tc.wantField)
		})
	}
}

// --- rows --------------------------------------------------------------

// TestBuildManifestMapsRowFields pins the discovery-to-row mapping.
func TestBuildManifestMapsRowFields(t *testing.T) {
	w := item("design-hub", "design", "design:hub", "active", "next")

	manifest, err := BuildManifest(snapshotInput([]workitem.WorkItem{w}))

	require.NoError(t, err)
	require.Len(t, manifest.Rows, 1)
	row := manifest.Rows[0]
	assert.Equal(t, "design-hub", row.StableID)
	assert.Equal(t, workitem.Derive("design-hub"), row.Ref)
	assert.Equal(t, "design:hub", row.Key)
	assert.Equal(t, "design", row.Type)
	assert.Equal(t, "workflow/design/design:hub", row.RelativePath)
	assert.Equal(t, "active", row.LifecycleStage)
	assert.Equal(t, "next", row.AttentionStage)
	assert.Nil(t, row.CarriedFrom)
	assert.Nil(t, row.IdentityException)
	assert.Equal(t, ProfileNameDefault, manifest.Profile.Name)
	assert.Equal(t, DefaultProfile().Review, manifest.Profile.Resolved.Review)
}

// TestRowPolicyFollowsTheLane: the default profile reads active lanes deeply
// and parked ones from metadata, and a row records the depth it was assigned
// so the choice survives a later profile change.
func TestRowPolicyFollowsTheLane(t *testing.T) {
	tests := []struct {
		attention string
		want      EvidenceDepth
	}{
		{"current", EvidenceDepthDeep},
		{"next", EvidenceDepthDeep},
		{"active", EvidenceDepthDeep},
		{"parked", EvidenceDepthMetadata},
		{"", EvidenceDepthMetadata},
	}

	for _, tc := range tests {
		t.Run("attention="+tc.attention, func(t *testing.T) {
			w := item("design-a", "design", "design:a", "active", tc.attention)

			manifest, err := BuildManifest(snapshotInput([]workitem.WorkItem{w}))

			require.NoError(t, err)
			assert.Equal(t, tc.want, manifest.Rows[0].Policy.Evidence)
			assert.Equal(t, RoutingTierCheap, manifest.Rows[0].Policy.RoutingTier)
		})
	}
}

// TestUnadoptedDocGetsPathBoundIdentity is FT-008 as mechanism: a design or
// explore directory with no marker still triages, but the run records that its
// identity is its path so nothing downstream mistakes the fallback for a
// durable id.
func TestUnadoptedDocGetsPathBoundIdentity(t *testing.T) {
	w := item("design-legacy", "design", "design:legacy", "none", "")
	w.StableID = ""
	w.SourceID = ""
	w.SourceMetadata = nil

	manifest, err := BuildManifest(snapshotInput([]workitem.WorkItem{w}))

	require.NoError(t, err)
	row := manifest.Rows[0]
	assert.Equal(t, "design:legacy", row.StableID, "the key stands in for the id")
	assert.Empty(t, row.Ref)
	require.NotNil(t, row.IdentityException)
	assert.Equal(t, w.RelativePath, row.IdentityException.Path)
	assert.Contains(t, row.IdentityException.Reason, "path")
	assert.Equal(t, 1, IdentityExceptionCount(manifest))
}

// TestMarkerlessTypesAreNotExceptions is the other half, and the one that
// keeps the field meaningful: intents and festivals never carry a .workitem
// marker, so flagging them would put an exception on most of a real campaign.
// Their identity is the source id discovery already resolved.
func TestMarkerlessTypesAreNotExceptions(t *testing.T) {
	tests := []struct {
		wfType string
		stage  string
	}{
		{"intent", "inbox"},
		{"festival", "active"},
	}

	for _, tc := range tests {
		t.Run(tc.wfType, func(t *testing.T) {
			w := item("x", tc.wfType, tc.wfType+":a", tc.stage, "")
			w.StableID = ""
			w.SourceID = tc.wfType + "-source-id"
			w.SourceMetadata = nil

			manifest, err := BuildManifest(snapshotInput([]workitem.WorkItem{w}))

			require.NoError(t, err)
			row := manifest.Rows[0]
			assert.Equal(t, tc.wfType+"-source-id", row.StableID,
				"the source id is a real identity, not a fallback")
			assert.Nil(t, row.IdentityException,
				"a type that never has a marker is not defective for lacking one")
			assert.Equal(t, 0, IdentityExceptionCount(manifest))
		})
	}
}

// --- partition ---------------------------------------------------------

// TestBatchesAreStableRegardlessOfDiscoveryOrder is the determinism guarantee
// the partition exists for: batch numbers must not depend on the order the
// filesystem walk happened to produce, or an incremental run could not compare
// rows across runs at all.
func TestBatchesAreStableRegardlessOfDiscoveryOrder(t *testing.T) {
	items := []workitem.WorkItem{
		item("design-c", "design", "design:c", "active", "active"),
		item("intent-a", "intent", "intent:a", "inbox", "next"),
		item("design-a", "design", "design:a", "active", "active"),
		item("intent-b", "intent", "intent:b", "inbox", "next"),
		item("design-b", "design", "design:b", "active", "active"),
	}
	reversed := make([]workitem.WorkItem, len(items))
	for i, w := range items {
		reversed[len(items)-1-i] = w
	}

	first, err := BuildManifest(snapshotInput(items))
	require.NoError(t, err)
	second, err := BuildManifest(snapshotInput(reversed))
	require.NoError(t, err)

	assert.Equal(t, first.Rows, second.Rows)

	// The store assigns the run id, so give both the same one to compare the
	// bytes the snapshot is actually responsible for.
	first.RunID, second.RunID = "run-1", "run-1"
	firstBytes, err := MarshalDocument(first)
	require.NoError(t, err)
	secondBytes, err := MarshalDocument(second)
	require.NoError(t, err)
	assert.Equal(t, string(firstBytes), string(secondBytes),
		"two snapshots of the same campaign must be byte-identical")
}

// TestBatchesChunkWithinLanes: with group_by type and batch_size 5, each type
// starts a new batch and fills to the batch size before the next one opens.
func TestBatchesChunkWithinLanes(t *testing.T) {
	var items []workitem.WorkItem
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		items = append(items, item("design-"+name, "design", "design:"+name, "active", "active"))
	}
	items = append(items, item("intent-a", "intent", "intent:a", "inbox", "next"))

	manifest, err := BuildManifest(snapshotInput(items))

	require.NoError(t, err)
	byKey := map[string]int{}
	for _, row := range manifest.Rows {
		byKey[row.Key] = row.Batch
	}
	// Five designs fill batch 1, the sixth opens batch 2, and the intent lane
	// starts a batch of its own rather than topping up the design batch.
	assert.Equal(t, 1, byKey["design:a"])
	assert.Equal(t, 1, byKey["design:e"])
	assert.Equal(t, 2, byKey["design:f"])
	assert.Equal(t, 3, byKey["intent:a"])
	assert.Equal(t, 3, BatchCount(manifest))
}

// TestGroupByAttentionStagePartitionsByLane covers the other grouping the
// profile offers.
func TestGroupByAttentionStagePartitionsByLane(t *testing.T) {
	profile := DefaultProfile()
	profile.Review.GroupBy = GroupByAttentionStage
	profile.Review.BatchSize = 10

	in := snapshotInput([]workitem.WorkItem{
		item("design-a", "design", "design:a", "active", "current"),
		item("intent-a", "intent", "intent:a", "inbox", "current"),
		item("design-b", "design", "design:b", "active", "parked"),
	})
	in.Profile = profile

	manifest, err := BuildManifest(in)

	require.NoError(t, err)
	byKey := map[string]int{}
	for _, row := range manifest.Rows {
		byKey[row.Key] = row.Batch
	}
	assert.Equal(t, byKey["design:a"], byKey["intent:a"], "same lane, same batch")
	assert.NotEqual(t, byKey["design:a"], byKey["design:b"], "different lane, different batch")
}

// TestEveryRowIsBatched: an unbatched row is invalid, so this also guards the
// grouping from silently dropping a row.
func TestEveryRowIsBatched(t *testing.T) {
	items := []workitem.WorkItem{
		item("design-a", "design", "design:a", "active", "active"),
		item("explore-a", "explore", "explore:a", "none", ""),
		item("festival-a", "festival", "festival:a", "active", "parked"),
	}

	manifest, err := BuildManifest(snapshotInput(items))

	require.NoError(t, err)
	require.Len(t, manifest.Rows, 3)
	for _, row := range manifest.Rows {
		assert.GreaterOrEqual(t, row.Batch, 1, "row %s unbatched", row.Key)
	}
}

// TestSnapshotScalesToACampaign is the phase's speed criterion in test form:
// the pure snapshot of a 150-row campaign is not where time goes.
func TestSnapshotScalesToACampaign(t *testing.T) {
	var items []workitem.WorkItem
	for i := range 150 {
		name := string(rune('a'+i%26)) + strconv.Itoa(i)
		items = append(items, item("design-"+name, "design", "design:"+name, "active", "active"))
	}

	manifest, err := BuildManifest(snapshotInput(items))

	require.NoError(t, err)
	assert.Len(t, manifest.Rows, 150)
	assert.Equal(t, 30, BatchCount(manifest), "150 rows at batch_size 5")
}
