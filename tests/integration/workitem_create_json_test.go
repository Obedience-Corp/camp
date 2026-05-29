//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_WorkitemCreate_JSON(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workitem-create-json"
	initWorkflowCampaign(t, tc, dir)

	out, err := tc.RunCampInDir(dir,
		"workitem", "create", "agent-test",
		"--type", "feature", "--title", "Agent Test", "--json")
	require.NoError(t, err, "workitem create --json: %s", out)

	start := strings.Index(out, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON in output: %s", out)
	payload := out[start:]

	var got struct {
		SchemaVersion string `json:"schema_version"`
		Workitem      struct {
			ID            string `json:"id"`
			Ref           string `json:"ref"`
			Type          string `json:"type"`
			Title         string `json:"title"`
			QuestID       string `json:"quest_id"`
			RelativePath  string `json:"relative_path"`
			MarkerVersion string `json:"marker_version"`
		} `json:"workitem"`
	}
	require.NoError(t, json.Unmarshal([]byte(payload), &got), "parse: %s", payload)

	assert.Equal(t, "workitem-create/v1alpha1", got.SchemaVersion)
	assert.NotEmpty(t, got.Workitem.ID, "id missing: %s", payload)
	assert.Regexp(t, `^WI-[0-9a-f]{6}$`, got.Workitem.Ref, "ref shape: %s", got.Workitem.Ref)
	assert.Equal(t, "feature", got.Workitem.Type)
	assert.Equal(t, "Agent Test", got.Workitem.Title)
	assert.Equal(t, "workflow/feature/agent-test", got.Workitem.RelativePath)
	assert.NotEmpty(t, got.Workitem.MarkerVersion, "marker_version missing: %s", payload)

	for _, key := range topLevelKeys(t, payload) {
		assert.False(t, isPascalCase(key),
			"JSON key %q is PascalCase; CW0003 contract requires snake_case", key)
	}

	resolveOut, err := tc.RunCampInDir(dir+"/workflow/feature/agent-test",
		"workitem", "resolve")
	require.NoError(t, err, "resolve: %s", resolveOut)
	assert.Contains(t, resolveOut, got.Workitem.ID,
		"resolver must find the workitem just created: %s", resolveOut)
}

func topLevelKeys(t *testing.T, raw string) []string {
	t.Helper()
	var generic map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &generic), "parse generic: %s", raw)
	var keys []string
	walk(generic, &keys)
	return keys
}

func walk(v any, out *[]string) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for k, child := range m {
		*out = append(*out, k)
		walk(child, out)
	}
}

func isPascalCase(s string) bool {
	if s == "" {
		return false
	}
	return unicode.IsUpper(rune(s[0]))
}
