package triage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

// writeAnchorFile creates a file under root and returns its campaign-relative
// path and the hash an anchor would record for it.
func writeAnchorFile(t *testing.T, root, rel, body string) string {
	t.Helper()
	abs := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(body), 0o644))

	hash, err := hashFile(abs)
	require.NoError(t, err)
	return hash
}

// TestCheckLocalAnchors covers each locally checkable kind and the remote kind
// that must degrade rather than guess.
func TestCheckLocalAnchors(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	hash := writeAnchorFile(t, root, "docs/anchored.md", "original contents\n")

	index := DiscoveryIndex{
		ByStableID: map[string]DiscoveredItem{
			"wi-tracked": {StableID: "wi-tracked", AttentionStage: "active"},
		},
		FestivalStatus: map[string]string{"CI0009": "completed"},
	}

	tests := []struct {
		name          string
		anchor        Anchor
		wantObserved  string
		wantUnchecked bool
		wantMatches   bool
	}{
		{
			name:         "path anchor matches its recorded hash",
			anchor:       Anchor{Kind: AnchorKindPath, Path: "docs/anchored.md", Hash: hash},
			wantObserved: hash,
			wantMatches:  true,
		},
		{
			name:         "path anchor reports a different hash when contents changed",
			anchor:       Anchor{Kind: AnchorKindPath, Path: "docs/anchored.md", Hash: PathHashPrefix + "stale"},
			wantObserved: hash,
			wantMatches:  false,
		},
		{
			name:         "a missing anchor file observes nothing",
			anchor:       Anchor{Kind: AnchorKindPath, Path: "docs/deleted.md", Hash: PathHashPrefix + "aaa"},
			wantObserved: "",
			wantMatches:  false,
		},
		{
			name:         "workitem anchor reads the stage from the discovery index",
			anchor:       Anchor{Kind: AnchorKindWorkitem, StableID: "wi-tracked", ObservedStage: "active"},
			wantObserved: "active",
			wantMatches:  true,
		},
		{
			name:         "workitem anchor notices an advanced stage",
			anchor:       Anchor{Kind: AnchorKindWorkitem, StableID: "wi-tracked", ObservedStage: "parked"},
			wantObserved: "active",
			wantMatches:  false,
		},
		{
			name:         "an undiscoverable workitem anchor observes nothing",
			anchor:       Anchor{Kind: AnchorKindWorkitem, StableID: "wi-vanished", ObservedStage: "active"},
			wantObserved: "",
			wantMatches:  false,
		},
		{
			name:         "festival anchor reads the status from the same walk",
			anchor:       Anchor{Kind: AnchorKindFestival, ID: "CI0009", Observed: "completed"},
			wantObserved: "completed",
			wantMatches:  true,
		},
		{
			name:         "festival anchor notices an advanced status",
			anchor:       Anchor{Kind: AnchorKindFestival, ID: "CI0009", Observed: "active"},
			wantObserved: "completed",
			wantMatches:  false,
		},
		{
			name: "a pr anchor is recorded unchecked rather than assumed",
			anchor: Anchor{
				Kind: AnchorKindPR, Repo: "Obedience-Corp/camp",
				Number: 546, Observed: "open",
			},
			wantObserved:  ObservedUncheckedOffline,
			wantUnchecked: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckLocalAnchors(ctx, root,
				map[string][]Anchor{"row": {tt.anchor}}, index)
			require.NoError(t, err)
			require.Len(t, got["row"], 1)

			check := got["row"][0]
			assert.Equal(t, tt.wantObserved, check.Observed)
			assert.Equal(t, tt.wantUnchecked, check.Unchecked)
			if !tt.wantUnchecked {
				assert.Equal(t, tt.wantMatches, check.Matches())
			}
		})
	}
}

// TestCheckLocalAnchorsHashesEachPathOnce pins the cost bound: spec doc 04
// budgets one hash pass, not one per row that happens to anchor the same file.
func TestCheckLocalAnchorsHashesEachPathOnce(t *testing.T) {
	root := t.TempDir()
	hash := writeAnchorFile(t, root, "docs/shared.md", "shared\n")

	shared := Anchor{Kind: AnchorKindPath, Path: "docs/shared.md", Hash: hash}
	byRow := map[string][]Anchor{
		"row-a": {shared, shared},
		"row-b": {shared},
		"row-c": {shared},
	}

	hasher := newPathHasher(root)
	for _, anchors := range byRow {
		for range anchors {
			_, err := hasher.hash("docs/shared.md")
			require.NoError(t, err)
		}
	}
	assert.Len(t, hasher.cache, 1, "one distinct path is hashed once")

	got, err := CheckLocalAnchors(context.Background(), root, byRow, DiscoveryIndex{})
	require.NoError(t, err)
	assert.Len(t, got, 3)
	for _, checks := range got {
		for _, check := range checks {
			assert.True(t, check.Matches())
		}
	}
}

// TestCheckLocalAnchorsHonorsCancellation is the context rule: a long anchor
// pass must stop when the caller gives up.
func TestCheckLocalAnchorsHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CheckLocalAnchors(ctx, t.TempDir(),
		map[string][]Anchor{"row": {{Kind: AnchorKindPath, Path: "a.md", Hash: PathHashPrefix + "x"}}},
		DiscoveryIndex{})
	assert.ErrorIs(t, err, context.Canceled)
}

// TestCheckLocalAnchorsSkipsRowsWithoutAnchors keeps the result map free of
// empty entries, so "has no anchors" and "has anchors that all matched" are
// not the same shape downstream.
func TestCheckLocalAnchorsSkipsRowsWithoutAnchors(t *testing.T) {
	got, err := CheckLocalAnchors(context.Background(), t.TempDir(),
		map[string][]Anchor{"bare": {}}, DiscoveryIndex{})
	require.NoError(t, err)
	assert.NotContains(t, got, "bare")
}

// TestIndexDiscovery covers the single-walk index both the diff and the
// anchor checkers read.
func TestIndexDiscovery(t *testing.T) {
	items := []workitem.WorkItem{
		{
			Key: "design:workflow/design/live", WorkflowType: "design",
			StableID: "design-live", RelativePath: "workflow/design/live",
			LifecycleStage: "active", AttentionStage: "parked",
			Title: "Live design",
		},
		{
			Key: "festival:festivals/active/CI0009", WorkflowType: workitem.WorkflowTypeFestival,
			SourceID: "CI0009", RelativePath: "festivals/active/CI0009",
			LifecycleStage: "active",
		},
		{
			Key: "design:workflow/.dungeon/archived/old", WorkflowType: "design",
			StableID: "design-archived", RelativePath: "workflow/dungeon/archived/old",
		},
	}

	index := IndexDiscovery(items)

	assert.Contains(t, index.ByStableID, "design-live")
	assert.Contains(t, index.ByStableID, "CI0009",
		"a festival is indexed under its source id, matching StableIDFor")
	assert.NotContains(t, index.ByStableID, "design-archived",
		"items inside a dungeon are excluded; that is what gone means")

	assert.Equal(t, "active", index.FestivalStatus["CI0009"],
		"a festival's status is its lifecycle stage, from the same walk")

	live := index.ByStableID["design-live"]
	assert.Equal(t, "workflow/design/live", live.RelativePath)
	assert.Equal(t, "parked", live.AttentionStage)
}

// TestIndexDiscoveryNormalizesLifecycle is a regression guard: the manifest
// writes "none" for an absent lifecycle stage, so the index must too, or every
// such row would classify moved on every single refresh.
func TestIndexDiscoveryNormalizesLifecycle(t *testing.T) {
	item := workitem.WorkItem{
		Key: "intent:.campaign/intents/inbox/x.md", WorkflowType: "intent",
		SourceID: "CI0001", RelativePath: ".campaign/intents/inbox/x.md",
	}

	row := rowFor(item, DefaultProfile(), testAt)
	indexed := IndexDiscovery([]workitem.WorkItem{item}).ByStableID["CI0001"]

	assert.Equal(t, string(workitem.LifecycleStageNone), indexed.LifecycleStage)
	assert.Equal(t, row.LifecycleStage, indexed.LifecycleStage,
		"a manifest row and its re-discovery must agree, or the row never stops moving")

	diff := ClassifyRows(DiffInput{
		Rows:       []ManifestRow{row},
		Discovered: map[string]DiscoveredItem{"CI0001": indexed},
	})
	require.Len(t, diff.Rows, 1)
	assert.Equal(t, ClassFresh, diff.Rows[0].Class)
}

// TestStableIDForMatchesAcrossSnapshotAndRefresh is the identity rule stated
// as a test: the snapshot and the refresh index must resolve the same id for
// the same item, or refresh reports a legacy row gone on an unchanged campaign.
func TestStableIDForMatchesAcrossSnapshotAndRefresh(t *testing.T) {
	tests := []struct {
		name string
		item workitem.WorkItem
		want string
	}{
		{
			name: "marker stable id wins",
			item: workitem.WorkItem{
				StableID: "design-marked", SourceID: "ignored",
				Key: "design:workflow/design/marked", WorkflowType: "design",
			},
			want: "design-marked",
		},
		{
			name: "source id is the fallback for markerless types",
			item: workitem.WorkItem{
				SourceID: "CI0009", Key: "intent:.campaign/intents/inbox/x.md",
				WorkflowType: "intent",
			},
			want: "CI0009",
		},
		{
			name: "the discovery key is the last resort",
			item: workitem.WorkItem{
				Key: "design:workflow/design/bare", WorkflowType: "design",
			},
			want: "design:workflow/design/bare",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, StableIDFor(tt.item))

			row := rowFor(tt.item, DefaultProfile(), testAt)
			index := IndexDiscovery([]workitem.WorkItem{tt.item})
			assert.Equal(t, row.StableID, index.ByStableID[tt.want].StableID,
				"snapshot and refresh must key the same item identically")
		})
	}
}

// TestAnchorAccessors pins the union accessors the diff compares through.
func TestAnchorAccessors(t *testing.T) {
	tests := []struct {
		name       string
		anchor     Anchor
		wantValue  string
		wantTarget string
		wantString string
	}{
		{
			name:       "path anchors record the hash",
			anchor:     pathAnchor("docs/x.md", "aaa"),
			wantValue:  "sha256:aaa",
			wantTarget: "docs/x.md",
			wantString: "path:docs/x.md",
		},
		{
			name:       "workitem anchors record the stage",
			anchor:     Anchor{Kind: AnchorKindWorkitem, StableID: "wi-1", ObservedStage: "active"},
			wantValue:  "active",
			wantTarget: "wi-1",
			wantString: "workitem:wi-1",
		},
		{
			name:       "festival anchors record the status",
			anchor:     Anchor{Kind: AnchorKindFestival, ID: "CI0009", Observed: "active"},
			wantValue:  "active",
			wantTarget: "CI0009",
			wantString: "festival:CI0009",
		},
		{
			name:       "pr anchors record the state",
			anchor:     Anchor{Kind: AnchorKindPR, Repo: "Obedience-Corp/camp", Number: 546, Observed: "open"},
			wantValue:  "open",
			wantTarget: "Obedience-Corp/camp#546",
			wantString: "pr:Obedience-Corp/camp#546",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantValue, tt.anchor.RecordedValue())
			assert.Equal(t, tt.wantTarget, tt.anchor.Target())
			assert.Equal(t, tt.wantString, tt.anchor.String())
		})
	}
}
