//go:build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// itemAddResult mirrors the checklist mutation JSON payload enough to read the
// new item's id back out for follow-up commands.
type itemAddResult struct {
	SchemaVersion string `json:"schema_version"`
	Item          struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Rank   int    `json:"rank"`
	} `json:"item"`
}

func setupChecklistQuest(t *testing.T, path string) *TestContainer {
	t.Helper()
	tc := GetSharedContainer(t)
	_, err := tc.InitCampaign(path, "quest-checklist", "product")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(path, "quest", "create", "billing",
		"--no-editor", "--purpose", "Q3 billing revamp", "--no-commit")
	require.NoError(t, err, out)
	return tc
}

func TestQuestChecklist_AddListCompleteRoundTrip(t *testing.T) {
	path := "/campaigns/quest-checklist-crud"
	tc := setupChecklistQuest(t, path)

	// Add two items; capture the first item's id from its JSON result.
	addOut, err := tc.RunCampInDir(path, "quest", "item", "add", "billing",
		"Ship invoicing v1", "--no-commit", "--json")
	require.NoError(t, err, addOut)

	var first itemAddResult
	require.NoError(t, json.Unmarshal([]byte(addOut), &first), "add --json: %s", addOut)
	assert.Equal(t, "quest-checklist/v1alpha1", first.SchemaVersion)
	assert.Equal(t, "open", first.Item.Status)
	assert.Equal(t, 10, first.Item.Rank, "first item auto-ranks to 10")
	require.NotEmpty(t, first.Item.ID)

	_, err = tc.RunCampInDir(path, "quest", "item", "add", "billing",
		"Dunning emails", "--status", "doing", "--no-commit")
	require.NoError(t, err)

	// List reflects both items with their statuses.
	listOut, err := tc.RunCampInDir(path, "quest", "checklist", "billing", "--json")
	require.NoError(t, err, listOut)
	assert.Contains(t, listOut, "quest-checklist/v1alpha1")
	assert.Contains(t, listOut, "Ship invoicing v1")
	assert.Contains(t, listOut, "Dunning emails")
	assert.Contains(t, listOut, `"status": "doing"`)

	// Complete the first item and confirm --open hides it.
	doneOut, err := tc.RunCampInDir(path, "quest", "item", "done", "billing", first.Item.ID, "--no-commit")
	require.NoError(t, err, doneOut)

	openOut, err := tc.RunCampInDir(path, "quest", "checklist", "billing", "--open", "--json")
	require.NoError(t, err, openOut)
	assert.NotContains(t, openOut, "Ship invoicing v1", "completed item must not appear in --open")
	assert.Contains(t, openOut, "Dunning emails", "still-open item must appear in --open")

	// Reopen restores it to the open view.
	_, err = tc.RunCampInDir(path, "quest", "item", "reopen", "billing", first.Item.ID, "--no-commit")
	require.NoError(t, err)
	reopenOut, err := tc.RunCampInDir(path, "quest", "checklist", "billing", "--open", "--json")
	require.NoError(t, err, reopenOut)
	assert.Contains(t, reopenOut, "Ship invoicing v1", "reopened item must return to --open")
}

func TestQuestChecklist_WorkitemLinkResolvesAtReadTime(t *testing.T) {
	path := "/campaigns/quest-checklist-link"
	tc := setupChecklistQuest(t, path)

	// A real workitem to link against.
	wcOut, err := tc.RunCampInDir(path, "workitem", "create", "billing-dunning",
		"--type", "feature", "--title", "Dunning design")
	require.NoError(t, err, wcOut)

	addOut, err := tc.RunCampInDir(path, "quest", "item", "add", "billing",
		"Dunning emails", "--no-commit", "--json")
	require.NoError(t, err, addOut)
	var item itemAddResult
	require.NoError(t, json.Unmarshal([]byte(addOut), &item))

	// Link by directory slug; the stored id is the workitem's stable id.
	linkOut, err := tc.RunCampInDir(path, "quest", "item", "link-workitem", "billing",
		item.Item.ID, "billing-dunning", "--no-commit")
	require.NoError(t, err, linkOut)

	// Read model joins the workitem back in by id.
	listOut, err := tc.RunCampInDir(path, "quest", "checklist", "billing", "--json")
	require.NoError(t, err, listOut)
	assert.Contains(t, listOut, "feature-billing-dunning")
	assert.Contains(t, listOut, `"resolved_path"`)
	assert.Contains(t, listOut, "workflow/feature/billing-dunning")

	// Deleting the linked workitem must preserve its stored identity while
	// removing stale resolved data from the read-time join.
	_, exitCode, err := tc.ExecCommand("rm", "-rf", path+"/workflow/feature/billing-dunning")
	require.NoError(t, err)
	require.Zero(t, exitCode)
	missingOut, err := tc.RunCampInDir(path, "quest", "checklist", "billing", "--json")
	require.NoError(t, err, missingOut)
	assert.Contains(t, missingOut, "feature-billing-dunning")
	assert.Contains(t, missingOut, `"missing": true`)
	assert.NotContains(t, missingOut, `"resolved_path"`)

	// Unlink drops the reference.
	_, err = tc.RunCampInDir(path, "quest", "item", "unlink-workitem", "billing", item.Item.ID, "--no-commit")
	require.NoError(t, err)
	afterOut, err := tc.RunCampInDir(path, "quest", "checklist", "billing", "--json")
	require.NoError(t, err, afterOut)
	assert.NotContains(t, afterOut, "resolved_path", "unlinked item must have no workitem block")
}
