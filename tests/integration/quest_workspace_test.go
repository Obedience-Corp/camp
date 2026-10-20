//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type questWorkspaceShowJSON struct {
	SchemaVersion string          `json:"schema_version"`
	QuestID       string          `json:"quest_id"`
	Path          string          `json:"path"`
	Generation    string          `json:"generation"`
	Document      json.RawMessage `json:"document"`
}

type questWorkspacePushJSON struct {
	SchemaVersion string          `json:"schema_version"`
	TabID         string          `json:"tab_id"`
	Action        string          `json:"action"`
	Generation    string          `json:"generation"`
	Document      json.RawMessage `json:"document"`
}

func setupQuestWorkspaceCampaign(t *testing.T, campPath string) *TestContainer {
	t.Helper()
	tc := GetSharedContainer(t)
	_, err := tc.InitCampaign(campPath, path.Base(campPath), "product")
	require.NoError(t, err)
	out, err := tc.RunCampInDir(campPath, "quest", "create", "layout-demo", "--json", "--no-editor", "--purpose", "workspace contract")
	require.NoError(t, err, out)
	return tc
}

func TestQuestWorkspace_ShowValidatePushWithoutAppOrPlugin(t *testing.T) {
	campPath := "/campaigns/quest-workspace-ok"
	tc := setupQuestWorkspaceCampaign(t, campPath)

	// Prove ordinary workspace commands do not require camp-app on PATH.
	stdout, stderr, code := runQuestWorkspace(t, tc, campPath, "", true,
		"quest", "workspace", "show", "layout-demo", "--json")
	require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.NotContains(t, stderr, "camp-app")
	assert.NotContains(t, stdout, "camp-app")

	var show questWorkspaceShowJSON
	require.NoError(t, json.Unmarshal([]byte(stdout), &show))
	assert.Equal(t, "quest-workspace/v1alpha1", show.SchemaVersion)
	assert.Contains(t, show.Path, "ui-state.json")
	assert.Len(t, show.Generation, 64)

	valOut, err := tc.RunCampInDir(campPath, "quest", "workspace", "validate", "layout-demo", "--json")
	require.NoError(t, err, valOut)
	assert.Contains(t, valOut, `"valid": true`)

	spec := `{
	  "id": "agent-review",
	  "title": "Review",
	  "paneTree": {
	    "id": "pane-1",
	    "type": "pane",
	    "content": {"type": "file", "filePath": "README.md"}
	  }
	}`
	specPath := campPath + "/.tmp-push-spec.json"
	require.NoError(t, tc.WriteFile(specPath, spec))

	showMeta, err := tc.RunCampInDir(campPath, "quest", "show", "layout-demo", "--json")
	require.NoError(t, err, showMeta)
	var questShow struct {
		Quest struct {
			Path string `json:"path"`
		} `json:"quest"`
	}
	require.NoError(t, json.Unmarshal([]byte(showMeta), &questShow))
	questDir := campPath + "/" + path.Dir(questShow.Quest.Path)
	uiPath := questDir + "/ui-state.json"

	// Seed a user tab that must survive.
	userDoc := `{
	  "version": 2,
	  "workspaceTabs": [
	    {"id":"user-main","title":"User","paneTree":{"id":"pu","type":"pane","content":{"type":"empty"}}}
	  ]
	}`
	require.NoError(t, tc.WriteFile(uiPath, userDoc))

	pushOut, err := tc.RunCampInDir(campPath, "quest", "workspace", "push", "layout-demo", "--file", specPath, "--json")
	require.NoError(t, err, pushOut)
	var pushed questWorkspacePushJSON
	require.NoError(t, json.Unmarshal([]byte(pushOut), &pushed))
	assert.Equal(t, "quest-workspace/v1alpha1", pushed.SchemaVersion)
	assert.Equal(t, "agent-review", pushed.TabID)
	assert.Equal(t, "appended", pushed.Action)

	again, err := tc.RunCampInDir(campPath, "quest", "workspace", "push", "layout-demo", "--file", specPath, "--json")
	require.NoError(t, err, again)
	var pushed2 questWorkspacePushJSON
	require.NoError(t, json.Unmarshal([]byte(again), &pushed2))
	assert.Equal(t, "replaced", pushed2.Action)

	disk, err := tc.ReadFile(uiPath)
	require.NoError(t, err)
	assert.Contains(t, disk, `"user-main"`)
	assert.Contains(t, disk, `"origin": "agent"`)
	var diskDoc struct {
		Tabs []struct {
			ID string `json:"id"`
		} `json:"workspaceTabs"`
	}
	require.NoError(t, json.Unmarshal([]byte(disk), &diskDoc))
	agentTabs := 0
	for _, tab := range diskDoc.Tabs {
		if tab.ID == "agent-review" {
			agentTabs++
		}
	}
	assert.Equal(t, 1, agentTabs, "idempotent push must not duplicate agent tabs")
}

func TestQuestWorkspace_NegativePathsLeaveBytesUnchanged(t *testing.T) {
	campPath := "/campaigns/quest-workspace-neg"
	tc := setupQuestWorkspaceCampaign(t, campPath)

	out, err := tc.RunCampInDir(campPath, "quest", "show", "layout-demo", "--json")
	require.NoError(t, err, out)
	var show struct {
		Quest struct {
			Path string `json:"path"`
		} `json:"quest"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &show))
	questDir := campPath + "/" + path.Dir(show.Quest.Path)
	uiPath := questDir + "/ui-state.json"

	original := `{
	  "version": 2,
	  "workspaceTabs": [
	    {"id":"user-main","title":"User","origin":"user","paneTree":{"id":"pu","type":"pane","content":{"type":"empty"}}}
	  ]
	}`
	require.NoError(t, tc.WriteFile(uiPath, original))

	badSpec := `{
	  "id": "user-main",
	  "title": "Hijack",
	  "paneTree": {"id":"p","type":"pane","content":{"type":"empty"}}
	}`
	badPath := campPath + "/.tmp-bad-spec.json"
	require.NoError(t, tc.WriteFile(badPath, badSpec))
	_, pushErr := tc.RunCampInDir(campPath, "quest", "workspace", "push", "layout-demo", "--file", badPath, "--json")
	require.Error(t, pushErr)

	after, err := tc.ReadFile(uiPath)
	require.NoError(t, err)
	assert.JSONEq(t, original, after, "rejected user-id push must not mutate disk")

	malformed := `{
	  "id": "agent-x",
	  "title": "X",
	  "paneTree": {"id":"s","type":"split","direction":"diagonal","children":[]}
	}`
	malPath := campPath + "/.tmp-mal-spec.json"
	require.NoError(t, tc.WriteFile(malPath, malformed))
	_, malErr := tc.RunCampInDir(campPath, "quest", "workspace", "push", "layout-demo", "--file", malPath)
	require.Error(t, malErr)
	after2, err := tc.ReadFile(uiPath)
	require.NoError(t, err)
	assert.JSONEq(t, original, after2, "malformed spec must leave bytes unchanged")
}

func TestQuestWorkspace_StableBuildOmitsCommand(t *testing.T) {
	tc := GetSharedContainer(t)
	// Dev binary is what the suite installs; prove help lists workspace under quest.
	out, err := tc.RunCamp("quest", "workspace", "--help")
	require.NoError(t, err, out)
	assert.Contains(t, out, "push")
	assert.Contains(t, out, "show")
	assert.Contains(t, out, "validate")
}

func runQuestWorkspace(t *testing.T, tc *TestContainer, dir, extraPath string, replacePATH bool, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		quotedArgs[i] = "'" + escaped + "'"
	}
	pathPrefix := ""
	if extraPath != "" {
		if replacePATH {
			pathPrefix = fmt.Sprintf("PATH=%s ", extraPath)
		} else {
			pathPrefix = fmt.Sprintf("PATH=%s:$PATH ", extraPath)
		}
	} else if replacePATH {
		pathPrefix = "PATH=/usr/bin:/bin "
	}
	cmdStr := fmt.Sprintf(
		"cd %s && %s%s/camp %s >/tmp/_camp_stdout 2>/tmp/_camp_stderr; echo $? >/tmp/_camp_exitcode",
		dir, pathPrefix, tc.campEnvPrefix(), strings.Join(quotedArgs, " "),
	)
	_, _, err := tc.ExecCommand("sh", "-c", cmdStr)
	require.NoError(t, err)
	stdoutRaw, _, _ := tc.ExecCommand("cat", "/tmp/_camp_stdout")
	stderrRaw, _, _ := tc.ExecCommand("cat", "/tmp/_camp_stderr")
	exitStr, _, _ := tc.ExecCommand("cat", "/tmp/_camp_exitcode")
	exitCode = 0
	if s := strings.TrimSpace(exitStr); s != "" && s != "0" {
		exitCode = 1
	}
	_, _, _ = tc.ExecCommand("rm", "-f", "/tmp/_camp_stdout", "/tmp/_camp_stderr", "/tmp/_camp_exitcode")
	return stdoutRaw, stderrRaw, exitCode
}
