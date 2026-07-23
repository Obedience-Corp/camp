//go:build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// questShowResult mirrors the quest show --json payload enough to read the
// bound workitem link back out.
type questShowResult struct {
	Quest struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Links []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"links"`
	} `json:"quest"`
}

func setupBindingCampaign(t *testing.T, path string) *TestContainer {
	t.Helper()
	tc := GetSharedContainer(t)
	_, err := tc.InitCampaign(path, "quest-binding", "product")
	require.NoError(t, err)
	return tc
}

// TestQuestCreate_WorkitemBinding_BadSelectorFailsFast asserts a bad --workitem
// selector errors before any quest is written (no orphan quest).
func TestQuestCreate_WorkitemBinding_BadSelectorFailsFast(t *testing.T) {
	path := "/campaigns/quest-bind-badsel"
	tc := setupBindingCampaign(t, path)

	out, err := tc.RunCampInDir(path, "quest", "create", "orphan",
		"--no-editor", "--workitem", "does-not-exist", "--no-commit")
	require.Error(t, err, "bad selector must fail: %s", out)
	assert.Contains(t, out, "does-not-exist")

	// The quest must not have been created.
	showOut, showErr := tc.RunCampInDir(path, "quest", "show", "orphan", "--json")
	require.Error(t, showErr, "orphan quest must not exist: %s", showOut)
}

// TestQuestCreate_WorkitemBinding_BySlug binds a workitem selected by directory
// slug and verifies it renders through quest show/links with no extra steps.
func TestQuestCreate_WorkitemBinding_BySlug(t *testing.T) {
	path := "/campaigns/quest-bind-slug"
	tc := setupBindingCampaign(t, path)

	wcOut, err := tc.RunCampInDir(path, "workitem", "create", "billing-redesign",
		"--type", "design", "--title", "Billing redesign")
	require.NoError(t, err, wcOut)

	createOut, err := tc.RunCampInDir(path, "quest", "create", "launch",
		"--no-editor", "--purpose", "Q3 launch", "--workitem", "billing-redesign", "--no-commit")
	require.NoError(t, err, createOut)
	assert.Contains(t, createOut, "bound workitem:")
	assert.Contains(t, createOut, "workflow/design/billing-redesign")

	showOut, err := tc.RunCampInDir(path, "quest", "show", "launch", "--json")
	require.NoError(t, err, showOut)

	var show questShowResult
	require.NoError(t, json.Unmarshal([]byte(showOut), &show), "show --json: %s", showOut)
	require.Len(t, show.Quest.Links, 1, "expected exactly one bound link: %s", showOut)
	assert.Contains(t, show.Quest.Links[0].Path, "workflow/design/billing-redesign")
	assert.Equal(t, "design", show.Quest.Links[0].Type, "auto-detected link type for a design workitem")

	// quest links renders the same binding.
	linksOut, err := tc.RunCampInDir(path, "quest", "links", "launch", "--json")
	require.NoError(t, err, linksOut)
	assert.Contains(t, linksOut, "workflow/design/billing-redesign")
}

// TestQuestCreate_WorkitemBinding_ByPath binds a workitem selected by its
// campaign-relative path, the same resolver family the workitem commands use.
func TestQuestCreate_WorkitemBinding_ByPath(t *testing.T) {
	path := "/campaigns/quest-bind-path"
	tc := setupBindingCampaign(t, path)

	wcOut, err := tc.RunCampInDir(path, "workitem", "create", "search-index",
		"--type", "feature", "--title", "Search index")
	require.NoError(t, err, wcOut)

	createOut, err := tc.RunCampInDir(path, "quest", "create", "indexing",
		"--no-editor", "--workitem", "workflow/feature/search-index", "--no-commit")
	require.NoError(t, err, createOut)

	showOut, err := tc.RunCampInDir(path, "quest", "show", "indexing", "--json")
	require.NoError(t, err, showOut)
	var show questShowResult
	require.NoError(t, json.Unmarshal([]byte(showOut), &show), "show --json: %s", showOut)
	require.Len(t, show.Quest.Links, 1, "expected one bound link: %s", showOut)
	assert.Contains(t, show.Quest.Links[0].Path, "workflow/feature/search-index")
}

// TestQuestCreate_WorkitemBinding_CommittedInCreateCommit asserts the binding is
// written into the same create commit and leaves no dirty quest file behind.
func TestQuestCreate_WorkitemBinding_CommittedInCreateCommit(t *testing.T) {
	path := "/campaigns/quest-bind-commit"
	tc := setupBindingCampaign(t, path)

	wcOut, err := tc.RunCampInDir(path, "workitem", "create", "telemetry",
		"--type", "design", "--title", "Telemetry")
	require.NoError(t, err, wcOut)

	createOut, err := tc.RunCampInDir(path, "quest", "create", "observability",
		"--no-editor", "--workitem", "telemetry")
	require.NoError(t, err, createOut)

	// The binding is present (proves the link was saved).
	showOut, err := tc.RunCampInDir(path, "quest", "show", "observability", "--json")
	require.NoError(t, err, showOut)
	var show questShowResult
	require.NoError(t, json.Unmarshal([]byte(showOut), &show), "show --json: %s", showOut)
	require.Len(t, show.Quest.Links, 1, "expected one bound link: %s", showOut)

	// No quest.yaml remains uncommitted: the link landed in the create commit.
	statusOut, exitCode, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	require.Zero(t, exitCode, statusOut)
	assert.NotContains(t, statusOut, ".campaign/quests",
		"quest binding must be committed, not left dirty: %q", statusOut)
}
