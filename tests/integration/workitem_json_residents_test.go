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

	assert.Equal(t, "workitems/v1alpha10", wiList(t, tc, path).SchemaVersion)
}

// The resident row's shape is asserted from real command output so casing is
// verified rather than assumed.
func TestWorkitemJSON_ResidentRowShape(t *testing.T) {
	tc := GetSharedContainer(t)
	path := setupRailCampaign(t, tc, "wi-json-resident")
	railTo(t, tc, path+"/workflow/design/rail-feature", "active")

	payload := wiList(t, tc, path)
	assert.Equal(t, "workitems/v1alpha10", payload.SchemaVersion)

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
