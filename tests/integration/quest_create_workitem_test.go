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
		ID          string `json:"id"`
		Name        string `json:"name"`
		Purpose     string `json:"purpose"`
		Description string `json:"description"`
		Links       []struct {
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

// TestQuestCreate_WorkitemEnrichment_FillsEmptyFields asserts that creating a
// quest with no purpose/description, then binding a workitem with a title,
// results in the quest's purpose being auto-populated from the workitem title.
// This is the camp#583 acceptance criterion: placeholder metadata is updated
// once purpose is determined.
func TestQuestCreate_WorkitemEnrichment_FillsEmptyFields(t *testing.T) {
	path := "/campaigns/quest-enrich-fill"
	tc := setupBindingCampaign(t, path)

	wcOut, err := tc.RunCampInDir(path, "workitem", "create", "api-gateway",
		"--type", "design", "--title", "API Gateway Redesign")
	require.NoError(t, err, wcOut)

	// Create a quest with empty purpose and description, binding the workitem.
	createOut, err := tc.RunCampInDir(path, "quest", "create", "gateway-quest",
		"--no-editor", "--workitem", "api-gateway", "--no-commit")
	require.NoError(t, err, createOut)

	showOut, err := tc.RunCampInDir(path, "quest", "show", "gateway-quest", "--json")
	require.NoError(t, err, showOut)

	var show questShowResult
	require.NoError(t, json.Unmarshal([]byte(showOut), &show), "show --json: %s", showOut)
	assert.Equal(t, "API Gateway Redesign", show.Quest.Purpose,
		"empty purpose must be enriched from workitem title")
}

// TestQuestCreate_WorkitemEnrichment_PreservesUserPurpose asserts that a
// user-supplied purpose is never overwritten by workitem enrichment.
func TestQuestCreate_WorkitemEnrichment_PreservesUserPurpose(t *testing.T) {
	path := "/campaigns/quest-enrich-preserve"
	tc := setupBindingCampaign(t, path)

	wcOut, err := tc.RunCampInDir(path, "workitem", "create", "auth-refactor",
		"--type", "design", "--title", "Auth Refactor")
	require.NoError(t, err, wcOut)

	createOut, err := tc.RunCampInDir(path, "quest", "create", "auth-quest",
		"--no-editor", "--purpose", "My explicit purpose",
		"--workitem", "auth-refactor", "--no-commit")
	require.NoError(t, err, createOut)

	showOut, err := tc.RunCampInDir(path, "quest", "show", "auth-quest", "--json")
	require.NoError(t, err, showOut)

	var show questShowResult
	require.NoError(t, json.Unmarshal([]byte(showOut), &show), "show --json: %s", showOut)
	assert.Equal(t, "My explicit purpose", show.Quest.Purpose,
		"user-supplied purpose must not be overwritten")
}

// TestQuestCreate_WorkitemEnrichment_PreSetPurposeCommitsQuestFile asserts that
// creating a quest with --purpose already set still commits the quest file.
// Enrichment is a no-op in that case; the post-Link Files must not be dropped.
func TestQuestCreate_WorkitemEnrichment_PreSetPurposeCommitsQuestFile(t *testing.T) {
	path := "/campaigns/quest-enrich-preset-create-commit"
	tc := setupBindingCampaign(t, path)

	wcOut, err := tc.RunCampInDir(path, "workitem", "create", "preset-design",
		"--type", "design", "--title", "Preset Design")
	require.NoError(t, err, wcOut)

	require.NoError(t, tc.WriteFile(path+"/unrelated-dirty.txt", "leave me unstaged\n"))

	createOut, err := tc.RunCampInDir(path, "quest", "create", "preset-quest",
		"--no-editor", "--purpose", "My explicit purpose",
		"--workitem", "preset-design")
	require.NoError(t, err, createOut)

	showOut, err := tc.RunCampInDir(path, "quest", "show", "preset-quest", "--json")
	require.NoError(t, err, showOut)
	var show questShowResult
	require.NoError(t, json.Unmarshal([]byte(showOut), &show), "show --json: %s", showOut)
	assert.Equal(t, "My explicit purpose", show.Quest.Purpose,
		"user-supplied purpose must not be overwritten")
	require.Len(t, show.Quest.Links, 1, "expected one bound link: %s", showOut)

	statusOut, exitCode, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	require.Zero(t, exitCode, statusOut)
	assert.NotContains(t, statusOut, ".campaign/quests",
		"quest file must be committed even when Purpose was pre-set: %q", statusOut)
	assert.Contains(t, statusOut, "unrelated-dirty.txt",
		"unrelated dirty file must not be swept into a non-selective commit: %q", statusOut)

	treeOut, exitCode, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && git ls-tree -r HEAD --name-only")
	require.NoError(t, err)
	require.Zero(t, exitCode, treeOut)
	assert.Contains(t, treeOut, ".campaign/quests/",
		"committed tree must include the quest file: %q", treeOut)
	assert.NotContains(t, treeOut, "unrelated-dirty.txt")
}

// TestQuestLink_WorkitemEnrichment_PreSetPurposeCommitsQuestFile is the link
// counterpart: a quest that already has Purpose still stages/commits the
// quest file after `camp quest link`.
func TestQuestLink_WorkitemEnrichment_PreSetPurposeCommitsQuestFile(t *testing.T) {
	path := "/campaigns/quest-enrich-preset-link-commit"
	tc := setupBindingCampaign(t, path)

	wcOut, err := tc.RunCampInDir(path, "workitem", "create", "preset-link",
		"--type", "design", "--title", "Preset Link")
	require.NoError(t, err, wcOut)

	createOut, err := tc.RunCampInDir(path, "quest", "create", "preset-link-quest",
		"--no-editor", "--purpose", "My explicit purpose", "--no-commit")
	require.NoError(t, err, createOut)

	require.NoError(t, tc.WriteFile(path+"/unrelated-dirty.txt", "leave me unstaged\n"))

	linkOut, err := tc.RunCampInDir(path, "quest", "link", "preset-link-quest",
		"workflow/design/preset-link")
	require.NoError(t, err, linkOut)

	showOut, err := tc.RunCampInDir(path, "quest", "show", "preset-link-quest", "--json")
	require.NoError(t, err, showOut)
	var show questShowResult
	require.NoError(t, json.Unmarshal([]byte(showOut), &show), "show --json: %s", showOut)
	assert.Equal(t, "My explicit purpose", show.Quest.Purpose)
	require.Len(t, show.Quest.Links, 1, "expected one bound link")

	statusOut, exitCode, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && git status --porcelain")
	require.NoError(t, err)
	require.Zero(t, exitCode, statusOut)
	assert.NotContains(t, statusOut, ".campaign/quests",
		"quest file must be committed after link when Purpose was pre-set: %q", statusOut)
	assert.Contains(t, statusOut, "unrelated-dirty.txt",
		"unrelated dirty file must not be swept into a non-selective commit: %q", statusOut)

	treeOut, exitCode, err := tc.ExecCommand("sh", "-c",
		"cd "+path+" && git ls-tree -r HEAD --name-only")
	require.NoError(t, err)
	require.Zero(t, exitCode, treeOut)
	assert.Contains(t, treeOut, ".campaign/quests/",
		"committed tree must include the quest file: %q", treeOut)
	assert.NotContains(t, treeOut, "unrelated-dirty.txt")
}

// TestQuestLink_WorkitemEnrichment_FillsEmptyFields asserts that linking a
// workitem to an existing quest (via `camp quest link`) also enriches empty
// metadata — the same enrichment applies whether the link happens at create
// time or afterwards.
func TestQuestLink_WorkitemEnrichment_FillsEmptyFields(t *testing.T) {
	path := "/campaigns/quest-enrich-link"
	tc := setupBindingCampaign(t, path)

	wcOut, err := tc.RunCampInDir(path, "workitem", "create", "cache-layer",
		"--type", "design", "--title", "Cache Layer Design")
	require.NoError(t, err, wcOut)

	// Create a quest first with no purpose/description.
	createOut, err := tc.RunCampInDir(path, "quest", "create", "cache-quest",
		"--no-editor", "--no-commit")
	require.NoError(t, err, createOut)

	// Then link the workitem.
	linkOut, err := tc.RunCampInDir(path, "quest", "link", "cache-quest",
		"workflow/design/cache-layer", "--no-commit")
	require.NoError(t, err, linkOut)

	showOut, err := tc.RunCampInDir(path, "quest", "show", "cache-quest", "--json")
	require.NoError(t, err, showOut)

	var show questShowResult
	require.NoError(t, json.Unmarshal([]byte(showOut), &show), "show --json: %s", showOut)
	assert.Equal(t, "Cache Layer Design", show.Quest.Purpose,
		"empty purpose must be enriched from workitem title after quest link")
	assert.Len(t, show.Quest.Links, 1, "expected one bound link")
}
