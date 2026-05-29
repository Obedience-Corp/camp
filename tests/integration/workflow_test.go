//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const workflowCampaignDir = "/test/workflow-full"

func initWorkflowCampaign(t *testing.T, tc *TestContainer, dir string) {
	t.Helper()
	_, err := tc.RunCamp(
		"init", dir,
		"--name", "Workflow Test",
		"--type", "product",
		"-d", "Workflow integration",
		"-m", "Verify workflow surface",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init should succeed")
}

func assertScaffoldDirs(t *testing.T, tc *TestContainer, workflowDir string) {
	t.Helper()
	for _, sub := range []string{
		"dungeon/completed", "dungeon/archived", "dungeon/someday",
	} {
		dirPath := workflowDir + "/" + sub
		dirExists, err := tc.CheckDirExists(dirPath)
		require.NoError(t, err)
		assert.True(t, dirExists, "status dir missing: %s", dirPath)

		gitkeep := dirPath + "/.gitkeep"
		fileExists, err := tc.CheckFileExists(gitkeep)
		require.NoError(t, err)
		assert.True(t, fileExists, "gitkeep missing: %s", gitkeep)
	}
	for _, sub := range []string{"inbox", "active", "ready"} {
		dirPath := workflowDir + "/" + sub
		dirExists, err := tc.CheckDirExists(dirPath)
		require.NoError(t, err)
		assert.False(t, dirExists, "live bucket should not be scaffolded: %s", dirPath)
	}
}

func TestIntegration_WorkflowFullFlow(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := workflowCampaignDir
	initWorkflowCampaign(t, tc, dir)

	out, err := tc.RunCampInDir(dir,
		"workflow", "create", "research",
		"--shortcut", "re",
		"--title", "Research",
	)
	require.NoError(t, err, "workflow create: %s", out)
	assert.Contains(t, out, "created workflow/research")
	assert.Contains(t, out, "dungeon dirs:")

	assertScaffoldDirs(t, tc, dir+"/workflow/research")

	obey, err := tc.ReadFile(dir + "/workflow/research/OBEY.md")
	require.NoError(t, err)
	assert.Contains(t, obey, "Research")

	jumps, err := tc.ReadFile(dir + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	assert.Contains(t, jumps, "re:")
	assert.Contains(t, jumps, "path: workflow/research/")

	campaignYAML, err := tc.ReadFile(dir + "/.campaign/campaign.yaml")
	require.NoError(t, err)
	assert.Contains(t, campaignYAML, "name: research")

	out, err = tc.RunCampInDir(dir,
		"workitem", "create", "compare-llms",
		"--type", "research",
		"--title", "Compare LLMs",
	)
	require.NoError(t, err, "workitem create: %s", out)
	assert.Contains(t, out, "created workflow/research/compare-llms")

	markerExists, err := tc.CheckFileExists(dir + "/workflow/research/compare-llms/.workitem")
	require.NoError(t, err)
	assert.True(t, markerExists, ".workitem marker missing")

	out, err = tc.RunCampInDir(dir, "complete", "re")
	require.NoError(t, err, "complete re: %s", out)
	assert.Contains(t, out, "compare-llms")

	out, err = tc.RunCampInDir(dir, "go", "re", "compare-llms", "--print")
	require.NoError(t, err, "go re compare-llms --print: %s", out)
	assert.Contains(t, out, "workflow/research/compare-llms")
}

func TestIntegration_WorkflowCreateTerminalDungeonDirs(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workflow-scaffold"
	initWorkflowCampaign(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workflow", "create", "research", "--shortcut", "re")
	require.NoError(t, err, "workflow create: %s", out)

	assertScaffoldDirs(t, tc, dir+"/workflow/research")
}

func TestIntegration_WorkflowCreateIdempotent(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workflow-idempotent"
	initWorkflowCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "workflow", "create", "research", "--shortcut", "re")
	require.NoError(t, err)

	// Capture mtimes after first create.
	before, _, err := tc.ExecCommand("sh", "-c",
		"stat -c %Y "+dir+"/.campaign/settings/jumps.yaml "+
			dir+"/.campaign/campaign.yaml "+
			dir+"/workflow/research/dungeon/completed/.gitkeep")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(dir, "workflow", "create", "research", "--shortcut", "re")
	require.NoError(t, err, "rerun: %s", out)
	assert.Contains(t, out, "no changes for workflow research")

	after, _, err := tc.ExecCommand("sh", "-c",
		"stat -c %Y "+dir+"/.campaign/settings/jumps.yaml "+
			dir+"/.campaign/campaign.yaml "+
			dir+"/workflow/research/dungeon/completed/.gitkeep")
	require.NoError(t, err)
	assert.Equal(t, before, after, "rerun changed file mtimes")
}

func TestIntegration_WorkflowList(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workflow-list"
	initWorkflowCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "workflow", "create", "research", "--shortcut", "re")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workflow", "create", "incident", "--shortcut", "in")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workitem", "create", "alpha", "--type", "research")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workitem", "create", "beta", "--type", "incident")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(dir, "workflow", "list")
	require.NoError(t, err, "list: %s", out)
	assert.Contains(t, out, "TYPE")
	assert.Contains(t, out, "research")
	assert.Contains(t, out, "incident")

	out, err = tc.RunCampInDir(dir, "workflow", "list", "--json")
	require.NoError(t, err, "list --json: %s", out)
	var payload struct {
		Workflows []map[string]any `json:"workflows"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &payload), "raw: %s", out)
	require.Len(t, payload.Workflows, 2)

	byType := map[string]map[string]any{}
	for _, w := range payload.Workflows {
		byType[w["type"].(string)] = w
	}
	require.Contains(t, byType, "research")
	require.Contains(t, byType, "incident")
	assert.Equal(t, float64(1), byType["research"]["workitem_count"], "research items")
	assert.Equal(t, float64(1), byType["incident"]["workitem_count"], "incident items")
}

func TestIntegration_WorkflowShow(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workflow-show"
	initWorkflowCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "workflow", "create", "research", "--shortcut", "re", "--title", "Research")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workitem", "create", "alpha", "--type", "research")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workitem", "create", "beta", "--type", "research")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(dir, "workflow", "show", "research")
	require.NoError(t, err, "show: %s", out)
	assert.Contains(t, out, "workflow: research")
	assert.Contains(t, out, "shortcut: re ->")
	assert.Contains(t, out, "workitems: 2")
	assert.Contains(t, out, "alpha")
	assert.Contains(t, out, "beta")

	out, err = tc.RunCampInDir(dir, "workflow", "show", "research", "--json")
	require.NoError(t, err, "show --json: %s", out)
	var payload struct {
		Type     string `json:"type"`
		Shortcut string `json:"shortcut"`
		Recent   []struct {
			Slug string `json:"slug"`
		} `json:"recent"`
		WorkitemCount int `json:"workitem_count"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &payload), "raw: %s", out)
	assert.Equal(t, "research", payload.Type)
	assert.Equal(t, "re", payload.Shortcut)
	assert.Equal(t, 2, payload.WorkitemCount)
	assert.Len(t, payload.Recent, 2)

	out, exitCode, err := tc.ExecCommand("sh", "-c", "cd "+dir+" && /camp workflow show missing 2>&1")
	require.NoError(t, err)
	assert.Equal(t, 2, exitCode, "workflow show missing output:\n%s", out)
	assert.Contains(t, out, "unknown workflow type: missing")
}

func TestIntegration_WorkflowShortcutAdd(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workflow-shortcut"
	initWorkflowCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "workflow", "create", "research", "--shortcut", "re")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(dir, "workflow", "shortcut", "add", "research", "alt")
	require.NoError(t, err, "shortcut add alt: %s", out)
	assert.Contains(t, out, "shortcut added: alt")

	jumps, err := tc.ReadFile(dir + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	assert.Contains(t, jumps, "re:", "original shortcut should remain")
	assert.Contains(t, jumps, "alt:", "new shortcut should be present")

	out, err = tc.RunCampInDir(dir, "workflow", "shortcut", "add", "research", "alt")
	require.NoError(t, err, "rerun: %s", out)
	assert.Contains(t, out, "no changes for shortcut alt")
}

func TestIntegration_WorkflowDoctorAndSync(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workflow-doctor"
	initWorkflowCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "workflow", "create", "research", "--shortcut", "re")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(dir, "workflow", "doctor")
	require.NoError(t, err, "clean doctor: %s", out)

	_, exitCode, err := tc.ExecCommand("rm", "-rf", dir+"/workflow/research")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)

	out, exitCode, err = tc.ExecCommand("sh", "-c", "cd "+dir+" && /camp workflow doctor 2>&1")
	require.NoError(t, err)
	assert.Equal(t, 2, exitCode, "doctor output:\n%s", out)
	assert.Contains(t, out, "workflow.shortcut.missing-target")

	out, err = tc.RunCampInDir(dir, "workflow", "sync")
	require.NoError(t, err, "sync dry-run: %s", out)
	assert.Contains(t, out, "would fix")

	jumpsBefore, err := tc.ReadFile(dir + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	assert.Contains(t, jumpsBefore, "re:", "dry-run must not remove shortcut")

	out, err = tc.RunCampInDir(dir, "workflow", "sync", "--apply")
	require.NoError(t, err, "sync --apply: %s", out)
	assert.Contains(t, out, "applied")

	jumpsAfter, err := tc.ReadFile(dir + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	assert.NotContains(t, jumpsAfter, "re:", "orphan shortcut should be removed")

	out, err = tc.RunCampInDir(dir, "workflow", "doctor")
	require.NoError(t, err, "doctor should be clean after sync --apply: %s", out)
}

func TestIntegration_WorkflowCreateDryRun(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workflow-dryrun"
	initWorkflowCampaign(t, tc, dir)

	// Read jumps.yaml after init so we have a stable baseline (camp init seeds
	// it with defaults; dry-run must not modify it from this state).
	jumpsBefore, err := tc.ReadFile(dir + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(dir, "workflow", "create", "research", "--shortcut", "re", "--dry-run")
	require.NoError(t, err, "dry-run: %s", out)
	assert.Contains(t, out, "plan: create workflow/research")
	assert.Contains(t, out, "dry run: nothing written")

	dirExists, err := tc.CheckDirExists(dir + "/workflow/research")
	require.NoError(t, err)
	assert.False(t, dirExists, "workflow/research must not exist after dry-run")

	jumpsAfter, err := tc.ReadFile(dir + "/.campaign/settings/jumps.yaml")
	require.NoError(t, err)
	assert.Equal(t, jumpsBefore, jumpsAfter, "jumps.yaml must be unchanged after dry-run")

	// Real run should succeed normally.
	out, err = tc.RunCampInDir(dir, "workflow", "create", "research", "--shortcut", "re")
	require.NoError(t, err, "create after dry-run: %s", out)
	assert.Contains(t, out, "created workflow/research")

	dirExists, err = tc.CheckDirExists(dir + "/workflow/research/inbox")
	require.NoError(t, err)
	assert.False(t, dirExists, "live bucket must not exist after real create")

	dirExists, err = tc.CheckDirExists(dir + "/workflow/research/dungeon/completed")
	require.NoError(t, err)
	assert.True(t, dirExists, "terminal dungeon scaffold must exist after real create")
}

func TestIntegration_WorkflowDoctor_DedupeHintMatchesApply(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workflow-dedupe-hint"
	initWorkflowCampaign(t, tc, dir)

	_, err := tc.RunCampInDir(dir, "workflow", "create", "research", "--shortcut", "re")
	require.NoError(t, err)

	jumpsPath := dir + "/.campaign/settings/jumps.yaml"
	orig, err := tc.ReadFile(jumpsPath)
	require.NoError(t, err)

	withDup := orig + "    RE:\n        path: workflow/research/\n        source: user\n"
	require.NoError(t, tc.WriteFile(jumpsPath, withDup))

	out, _ := tc.RunCampInDir(dir, "workflow", "doctor", "--json")
	var report struct {
		Findings []struct {
			Code    string `json:"code"`
			FixHint string `json:"fix_hint"`
		} `json:"findings"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &report), "doctor --json: %s", out)

	var hint string
	for _, f := range report.Findings {
		if f.Code == "workflow.shortcut.duplicate" {
			hint = f.FixHint
			break
		}
	}
	require.NotEmpty(t, hint, "expected workflow.shortcut.duplicate finding in: %s", out)
	assert.Contains(t, hint, "normalized (lowercase)",
		"hint must describe normalized-lowercase policy, got: %s", hint)

	syncOut, err := tc.RunCampInDir(dir, "workflow", "sync", "--apply")
	require.NoError(t, err, "sync --apply: %s", syncOut)

	jumpsAfter, err := tc.ReadFile(jumpsPath)
	require.NoError(t, err)
	assert.Contains(t, jumpsAfter, "re:",
		"hint claims auto-fix keeps lowercase 're', but it was removed:\n%s", jumpsAfter)
	assert.NotContains(t, jumpsAfter, "RE:",
		"hint claims auto-fix removes uppercase 'RE', but it survived:\n%s", jumpsAfter)
}

func TestIntegration_WorkflowCreate_DryRunPerActionLines(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/workflow-dryrun-lines"
	initWorkflowCampaign(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workflow", "create", "research",
		"--shortcut", "re", "--dry-run")
	require.NoError(t, err, "dry-run: %s", out)

	assert.Contains(t, out, "create dir workflow/research/",
		"per-action line for workflow dir missing: %s", out)
	for _, sub := range []string{"dungeon/completed", "dungeon/archived", "dungeon/someday"} {
		assert.Contains(t, out, "create dir workflow/research/"+sub+"/",
			"per-action line for scaffold dir %s missing: %s", sub, out)
		assert.Contains(t, out, "create file workflow/research/"+sub+"/.gitkeep",
			"per-action line for gitkeep in %s missing: %s", sub, out)
	}
	for _, sub := range []string{"inbox", "active", "ready"} {
		assert.NotContains(t, out, "workflow/research/"+sub+"/",
			"dry-run should not mention live bucket %s: %s", sub, out)
	}
	assert.Contains(t, out, "create file workflow/research/OBEY.md",
		"per-action line for OBEY.md missing: %s", out)
	assert.Contains(t, out, "create shortcut re -> workflow/research/",
		"per-action line for shortcut missing: %s", out)
}
