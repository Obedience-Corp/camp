//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wiListPayload mirrors only the fields this contract test asserts. Field names
// come from real command output, not from the Go struct tags.
type wiListPayload struct {
	SchemaVersion   string              `json:"schema_version"`
	Items           []wiListItem        `json:"items"`
	Counts          map[string]int      `json:"counts"`
	StageVocabulary map[string][]string `json:"stage_vocabulary"`
}

type wiListItem struct {
	Key            string `json:"key"`
	WorkflowType   string `json:"workflow_type"`
	LifecycleStage string `json:"lifecycle_stage"`
	RelativePath   string `json:"relative_path"`
	StableID       string `json:"stable_id"`
	ItemKind       string `json:"item_kind"`
}

func wiList(t *testing.T, tc *TestContainer, path string, args ...string) wiListPayload {
	t.Helper()
	out, err := tc.RunCampInDir(path, append([]string{"workitem", "list", "--json"}, args...)...)
	require.NoError(t, err, "camp workitem list --json: %s", out)
	var p wiListPayload
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &p), "must parse: %s", out)
	return p
}

func itemByPath(items []wiListItem, rel string) (wiListItem, bool) {
	for _, it := range items {
		if it.RelativePath == rel {
			return it, true
		}
	}
	return wiListItem{}, false
}

func TestWorkitemJSON_SchemaVersionIsV1Alpha10(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/wi-json-version"
	_, err := tc.InitCampaign(path, "wi-json-version", "product")
	require.NoError(t, err)

	assert.Equal(t, "workitems/v1alpha11", wiList(t, tc, path).SchemaVersion)
}

// The resident row's shape is asserted from real command output so casing is
// verified rather than assumed.
func TestWorkitemJSON_ResidentRowShape(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "wi-json-resident")
	railTo(t, tc, path+"/workflow/design/rail-feature", "active")

	payload := wiList(t, tc, path)
	assert.Equal(t, "workitems/v1alpha11", payload.SchemaVersion)

	got, ok := itemByPath(payload.Items, "festivals/active/rail-feature")
	require.True(t, ok, "resident missing from camp wi --json: %+v", payload.Items)

	assert.Equal(t, "design", got.WorkflowType, "resident keeps its original type")
	assert.Equal(t, "active", got.LifecycleStage, "stage comes from the folder")
	assert.Equal(t, "design:festivals/active/rail-feature", got.Key)
	assert.Equal(t, "design-rail-feature-fixed", got.StableID)
	assert.Equal(t, "directory", got.ItemKind)

	assert.Contains(t, payload.StageVocabulary["explore"], "ready",
		"explore gains ready in the published vocabulary")
}

// Counts tally per built-in type, so a design resident increments design, not
// festival: the rail must not make a workitem look like a festival.
func TestWorkitemJSON_ResidentCountsAsItsOwnType(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "wi-json-counts")

	before := wiList(t, tc, path).Counts
	railTo(t, tc, path+"/workflow/design/rail-feature", "active")
	after := wiList(t, tc, path).Counts

	assert.Equal(t, before["design"], after["design"], "design count survives the move")
	assert.Equal(t, before["festival"], after["festival"], "resident must not count as a festival")
	assert.Equal(t, before["total"], after["total"], "total is unchanged; the item moved, it did not multiply")
}

func TestWorkitemJSON_FiltersComposeOverResidents(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "wi-json-filters")

	out, err := tc.RunCampInDir(path, "workitem", "create", "stay-put",
		"--type", "design", "--title", "Stay Put", "--id", "design-stay-put-fixed")
	require.NoError(t, err, "create second design: %s", out)
	railTo(t, tc, path+"/workflow/design/rail-feature", "active")

	resident := "festivals/active/rail-feature"
	root := "workflow/design/stay-put"

	byType := wiList(t, tc, path, "--type", "design").Items
	_, ok := itemByPath(byType, resident)
	assert.True(t, ok, "--type design must find the resident")
	_, ok = itemByPath(byType, root)
	assert.True(t, ok, "--type design must still find the root item")

	// A workflow/ directory item is already "active by location", so --stage
	// active spans both homes. ready is the value only a resident can hold.
	composed := wiList(t, tc, path, "--type", "design", "--stage", "active").Items
	_, ok = itemByPath(composed, resident)
	assert.True(t, ok, "--type design --stage active must find the resident")
	_, ok = itemByPath(composed, root)
	assert.True(t, ok, "root directory items report active by location")

	railTo(t, tc, path+"/workflow/design/stay-put", "ready")
	ready := wiList(t, tc, path, "--type", "design", "--stage", "ready").Items
	_, ok = itemByPath(ready, "festivals/ready/stay-put")
	assert.True(t, ok, "--stage ready must find the ready resident")
	_, ok = itemByPath(ready, resident)
	assert.False(t, ok, "the active resident must not match --stage ready")
}

// A stage folder holds both kinds at once: residents classify by their marker,
// while a real festival and a marker-less directory stay festivals.
func TestWorkitemJSON_MixedStageFolderSplits(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "wi-json-mixed")

	out, err := tc.RunCampInDir(path, "workitem", "create", "topic",
		"--type", "explore", "--title", "Topic", "--id", "explore-topic-fixed")
	require.NoError(t, err, "create explore: %s", out)

	railTo(t, tc, path+"/workflow/design/rail-feature", "active")
	railTo(t, tc, path+"/workflow/explore/topic", "ready")

	require.NoError(t, tc.WriteFile(path+"/festivals/active/real-fest/fest.yaml",
		"version: \"1.0\"\nmetadata:\n  id: RF0001\n  name: Real Fest\n  festival_type: implementation\n"))
	require.NoError(t, tc.WriteFile(path+"/festivals/active/real-fest/FESTIVAL_GOAL.md", "# Goal\n"))
	require.NoError(t, tc.WriteFile(path+"/festivals/active/bare-dir/NOTES.md", "# notes\n"))

	items := wiList(t, tc, path).Items

	design, ok := itemByPath(items, "festivals/active/rail-feature")
	require.True(t, ok, "design resident missing: %+v", items)
	assert.Equal(t, "design", design.WorkflowType)
	assert.Equal(t, "active", design.LifecycleStage)
	assert.Equal(t, "design:festivals/active/rail-feature", design.Key)
	assert.Equal(t, "design-rail-feature-fixed", design.StableID)

	explore, ok := itemByPath(items, "festivals/ready/topic")
	require.True(t, ok, "explore resident missing: %+v", items)
	assert.Equal(t, "explore", explore.WorkflowType)
	assert.Equal(t, "ready", explore.LifecycleStage)
	assert.Equal(t, "explore:festivals/ready/topic", explore.Key)
	assert.Equal(t, "explore-topic-fixed", explore.StableID)

	fest, ok := itemByPath(items, "festivals/active/real-fest")
	require.True(t, ok, "festival missing: %+v", items)
	assert.Equal(t, "festival", fest.WorkflowType)
	assert.Equal(t, "festival:festivals/active/real-fest", fest.Key)

	bare, ok := itemByPath(items, "festivals/active/bare-dir")
	require.True(t, ok, "marker-less dir missing: %+v", items)
	assert.Equal(t, "festival", bare.WorkflowType, "no marker means it stays a festival")
}

// A stamped directory in planning is neither a resident nor a festival, so it
// must not appear at all.
func TestWorkitemJSON_StampedDirOutsideRailStagesAbsent(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "wi-json-outofstage")

	require.NoError(t, tc.WriteFile(path+"/festivals/planning/stray/.workitem",
		"version: v1alpha8\nkind: workitem\nid: design-stray-fixed\ntype: design\ntitle: Stray\n"))
	require.NoError(t, tc.WriteFile(path+"/festivals/planning/stray/README.md", "# Stray\n"))

	items := wiList(t, tc, path).Items
	_, ok := itemByPath(items, "festivals/planning/stray")
	assert.False(t, ok, "stamped dir outside a rail stage must not be emitted: %+v", items)
}

// A festival-only campaign must produce no resident rows: reclassification only
// triggers on a marker, so nothing under festivals/ changes type without one.
func TestWorkitemJSON_FestivalOnlyCampaignHasNoResidents(t *testing.T) {
	tc := GetSharedContainer(t)
	path := "/campaigns/wi-json-festonly"
	_, err := tc.InitCampaign(path, "wi-json-festonly", "product")
	require.NoError(t, err)

	require.NoError(t, tc.WriteFile(path+"/festivals/active/f-one/fest.yaml",
		"version: \"1.0\"\nmetadata:\n  id: F1\n  name: F One\n  festival_type: implementation\n"))
	require.NoError(t, tc.WriteFile(path+"/festivals/active/f-one/FESTIVAL_GOAL.md", "# Goal\n"))
	require.NoError(t, tc.WriteFile(path+"/festivals/planning/f-two/fest.yaml",
		"version: \"1.0\"\nmetadata:\n  id: F2\n  name: F Two\n  festival_type: implementation\n"))
	require.NoError(t, tc.WriteFile(path+"/festivals/planning/f-two/FESTIVAL_GOAL.md", "# Goal\n"))

	payload := wiList(t, tc, path)
	assert.Equal(t, "workitems/v1alpha11", payload.SchemaVersion)
	for _, it := range payload.Items {
		if strings.HasPrefix(it.RelativePath, "festivals/") {
			assert.Equal(t, "festival", it.WorkflowType,
				"%s must stay a festival without a marker", it.RelativePath)
		}
	}
	assert.Equal(t, 2, payload.Counts["festival"])
	assert.Equal(t, 0, payload.Counts["design"])
}
