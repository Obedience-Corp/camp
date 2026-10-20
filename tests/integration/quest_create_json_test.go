//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// questCreateJSONResult mirrors camp quest create --json (quest-create/v1alpha1).
type questCreateJSONResult struct {
	SchemaVersion string `json:"schema_version"`
	CampaignRoot  string `json:"campaign_root"`
	Quest         *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
		Slug string `json:"slug"`
	} `json:"quest"`
	Created bool    `json:"created"`
	Binding string  `json:"binding"`
	Commit  string  `json:"commit"`
	Error   *string `json:"error"`
}

func setupQuestCreateJSONCampaign(t *testing.T, campPath string) *TestContainer {
	t.Helper()
	tc := GetSharedContainer(t)
	_, err := tc.InitCampaign(campPath, path.Base(campPath), "product")
	require.NoError(t, err)
	return tc
}

// runQuestCreateJSON runs camp with optional PATH prefix and returns split streams.
// When replacePATH is true, PATH is set to extraPath only so reveal cannot find a
// host camp-app; otherwise extraPath is prepended.
func runQuestCreateJSON(t *testing.T, tc *TestContainer, dir, extraPath string, replacePATH bool, args ...string) (stdout, stderr string, exitCode int) {
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

func installCampAppStub(t *testing.T, tc *TestContainer, binDir, body string) {
	t.Helper()
	tc.Shell(t, fmt.Sprintf("mkdir -p %s", binDir))
	require.NoError(t, tc.WriteFile(binDir+"/camp-app", "#!/bin/sh\n"+body+"\n"))
	tc.Shell(t, "chmod +x "+binDir+"/camp-app")
}

func decodeQuestCreateJSON(t *testing.T, stdout string) questCreateJSONResult {
	t.Helper()
	var got questCreateJSONResult
	require.NoError(t, json.Unmarshal([]byte(stdout), &got), "stdout: %s", stdout)
	return got
}

// TestQuestCreateJSON_Success returns identity, binding notRequested, and commit
// completed through the full create/ledger/commit handler.
func TestQuestCreateJSON_Success(t *testing.T) {
	campPath := "/campaigns/quest-create-json-ok"
	tc := setupQuestCreateJSONCampaign(t, campPath)

	stdout, stderr, code := runQuestCreateJSON(t, tc, campPath, "", false,
		"quest", "create", "structured-ok", "--json", "--purpose", "prove json create")
	require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.NotContains(t, stdout, "✓ Quest created", "text success line must stay off in --json")
	assert.NotContains(t, stderr, "camp-app", "ordinary success must not mention app reveal")

	got := decodeQuestCreateJSON(t, stdout)
	assert.Equal(t, "quest-create/v1alpha1", got.SchemaVersion)
	require.NotNil(t, got.Quest)
	assert.Equal(t, "structured-ok", got.Quest.Name)
	assert.NotEmpty(t, got.Quest.ID)
	assert.NotEmpty(t, got.Quest.Path)
	assert.True(t, got.Created)
	assert.Equal(t, "notRequested", got.Binding)
	assert.Equal(t, "completed", got.Commit)
	assert.Nil(t, got.Error)

	showOut, err := tc.RunCampInDir(campPath, "quest", "show", got.Quest.ID, "--json")
	require.NoError(t, err, showOut)
	assert.Contains(t, showOut, got.Quest.ID)
}

// TestQuestCreateJSON_InvalidBindingFailsBeforeCreate keeps the existing
// fail-fast selector contract and emits the JSON error envelope.
func TestQuestCreateJSON_InvalidBindingFailsBeforeCreate(t *testing.T) {
	campPath := "/campaigns/quest-create-json-badbind"
	tc := setupQuestCreateJSONCampaign(t, campPath)

	stdout, stderr, code := runQuestCreateJSON(t, tc, campPath, "", false,
		"quest", "create", "orphan-json", "--json", "--workitem", "does-not-exist", "--no-commit")
	require.NotEqual(t, 0, code, "bad selector must fail: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stderr, `"schema_version": "quest-create/v1alpha1"`)
	assert.Contains(t, stderr, `"code": "validation_error"`)
	assert.Contains(t, stderr, `"message": "not found"`)
	assert.NotContains(t, stdout, `"created": true`)

	showOut, showErr := tc.RunCampInDir(campPath, "quest", "show", "orphan-json", "--json")
	require.Error(t, showErr, "quest must not exist after invalid binding: %s", showOut)
}

// TestQuestCreateJSON_FailedCommitKeepsQuest reports partial commit failure with
// quest identity and never attempts optional reveal.
func TestQuestCreateJSON_FailedCommitKeepsQuest(t *testing.T) {
	campPath := "/campaigns/quest-create-json-badcommit"
	tc := setupQuestCreateJSONCampaign(t, campPath)

	probeLog := "/tmp/quest-create-json-badcommit-probe"
	binDir := "/tmp/quest-create-json-badcommit-bin"
	tc.Shell(t, "rm -f "+probeLog)
	installCampAppStub(t, tc, binDir, "echo probed >> "+probeLog+"; exit 0")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		cat > .git/hooks/pre-commit <<'HOOK'
#!/bin/sh
echo "forced commit failure" >&2
exit 1
HOOK
		chmod +x .git/hooks/pre-commit
	`, campPath))

	stdout, stderr, code := runQuestCreateJSON(t, tc, campPath, binDir, false,
		"quest", "create", "partial-commit", "--json", "--purpose", "commit should fail", "--open")
	require.NotEqual(t, 0, code, "failed commit must be nonzero: stdout=%s stderr=%s", stdout, stderr)

	got := decodeQuestCreateJSON(t, stdout)
	require.NotNil(t, got.Quest)
	assert.True(t, got.Created)
	assert.Equal(t, "failed", got.Commit)
	require.NotNil(t, got.Error)
	assert.Contains(t, *got.Error, "auto-commit failed")
	assert.NotContains(t, stderr, "reveal", "failed commit must suppress optional reveal")

	exists, err := tc.CheckFileExists(probeLog)
	require.NoError(t, err)
	assert.False(t, exists, "failed commit must not spawn camp-app even with --open")

	showOut, err := tc.RunCampInDir(campPath, "quest", "show", got.Quest.ID, "--json")
	require.NoError(t, err, "quest must remain after commit failure: %s", showOut)
}

// TestQuestCreateJSON_MissingPluginOpenKeepsLocalSuccess warns and exits zero
// when --open cannot find camp-app.
func TestQuestCreateJSON_MissingPluginOpenKeepsLocalSuccess(t *testing.T) {
	campPath := "/campaigns/quest-create-json-missing-app"
	tc := setupQuestCreateJSONCampaign(t, campPath)

	// Core PATH without camp-app so reveal LookPath fails while git/coreutils remain.
	corePATH := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

	stdout, stderr, code := runQuestCreateJSON(t, tc, campPath, corePATH, true,
		"quest", "create", "missing-app", "--json", "--no-commit", "--open")
	require.Equal(t, 0, code, "local success must stay zero: stdout=%s stderr=%s", stdout, stderr)

	got := decodeQuestCreateJSON(t, stdout)
	require.NotNil(t, got.Quest)
	assert.True(t, got.Created)
	assert.Nil(t, got.Error)
	assert.Contains(t, stderr, got.Quest.ID)
	assert.Contains(t, stderr, "camp-app was not available")
	assert.Contains(t, stderr, "camp app quest open "+got.Quest.ID)
	assert.NotContains(t, strings.ToLower(stderr), "rerun")
}

// TestQuestCreateJSON_HungPluginOpenIsBounded keeps local success when camp-app
// hangs past the two-second reveal budget.
func TestQuestCreateJSON_HungPluginOpenIsBounded(t *testing.T) {
	campPath := "/campaigns/quest-create-json-hung-app"
	tc := setupQuestCreateJSONCampaign(t, campPath)

	binDir := "/tmp/quest-create-json-hung-bin"
	installCampAppStub(t, tc, binDir, "sleep 30")

	start := time.Now()
	stdout, stderr, code := runQuestCreateJSON(t, tc, campPath, binDir, false,
		"quest", "create", "hung-app", "--json", "--no-commit", "--open")
	elapsed := time.Since(start)

	require.Equal(t, 0, code, "hung reveal must not invalidate local success: stdout=%s stderr=%s", stdout, stderr)
	assert.Less(t, elapsed, 5*time.Second, "reveal budget must stay bounded; took %s", elapsed)

	got := decodeQuestCreateJSON(t, stdout)
	require.NotNil(t, got.Quest)
	assert.True(t, got.Created)
	assert.Contains(t, stderr, got.Quest.ID)
	assert.Contains(t, stderr, "reveal was not confirmed")
	assert.Contains(t, stderr, "camp app quest open "+got.Quest.ID)
}

// TestQuestCreateJSON_OrdinaryCreateNeverProbesApp proves create without --open
// never resolves or spawns camp-app even when a probe stub is first on PATH.
func TestQuestCreateJSON_OrdinaryCreateNeverProbesApp(t *testing.T) {
	campPath := "/campaigns/quest-create-json-no-probe"
	tc := setupQuestCreateJSONCampaign(t, campPath)

	probeLog := "/tmp/quest-create-json-no-probe.log"
	binDir := "/tmp/quest-create-json-no-probe-bin"
	tc.Shell(t, "rm -f "+probeLog)
	installCampAppStub(t, tc, binDir, "echo probed >> "+probeLog+"; exit 0")

	stdout, stderr, code := runQuestCreateJSON(t, tc, campPath, binDir, false,
		"quest", "create", "no-probe", "--json", "--no-commit")
	require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	got := decodeQuestCreateJSON(t, stdout)
	require.NotNil(t, got.Quest)
	assert.True(t, got.Created)

	exists, err := tc.CheckFileExists(probeLog)
	require.NoError(t, err)
	assert.False(t, exists, "create without --open must not spawn camp-app")
	assert.NotContains(t, stderr, "reveal")

	// Positive control: the same stub is reachable when --open is requested.
	stdout2, stderr2, code2 := runQuestCreateJSON(t, tc, campPath, binDir, false,
		"quest", "create", "probe-control", "--json", "--no-commit", "--open")
	require.Equal(t, 0, code2, "stdout:\n%s\nstderr:\n%s", stdout2, stderr2)
	exists, err = tc.CheckFileExists(probeLog)
	require.NoError(t, err)
	assert.True(t, exists, "--open must invoke camp-app for the positive control")
}

// TestQuestCreate_TextModePreservedWithoutJSON keeps the human create path and
// still supports bounded --open without changing text output shape.
func TestQuestCreate_TextModePreservedWithoutJSON(t *testing.T) {
	campPath := "/campaigns/quest-create-text-open"
	tc := setupQuestCreateJSONCampaign(t, campPath)

	corePATH := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

	stdout, stderr, code := runQuestCreateJSON(t, tc, campPath, corePATH, true,
		"quest", "create", "text-open", "--no-editor", "--no-commit", "--open")
	require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stdout, "✓ Quest created: text-open")
	assert.NotContains(t, stdout, `"schema_version"`)
	assert.Contains(t, stderr, "camp-app was not available")
}

// TestPluginCompletionStaysOptionalWithoutCampApp ensures ordinary completion
// does not require or probe camp-app.
func TestPluginCompletionStaysOptionalWithoutCampApp(t *testing.T) {
	tc := GetSharedContainer(t)

	probeLog := "/tmp/quest-create-completion-probe.log"
	binDir := "/tmp/quest-create-completion-bin"
	tc.Shell(t, "rm -f "+probeLog)
	installCampAppStub(t, tc, binDir, "echo probed >> "+probeLog+"; exit 1")

	cmdStr := fmt.Sprintf(
		`PATH=%s:$PATH /camp __complete quest create '' >/tmp/_camp_stdout 2>/tmp/_camp_stderr; echo $? >/tmp/_camp_exitcode`,
		binDir,
	)
	_, _, err := tc.ExecCommand("sh", "-c", cmdStr)
	require.NoError(t, err)
	exitStr, _, _ := tc.ExecCommand("cat", "/tmp/_camp_exitcode")
	stdout, _, _ := tc.ExecCommand("cat", "/tmp/_camp_stdout")
	require.Equal(t, "0", strings.TrimSpace(exitStr), "ordinary completion must succeed without camp-app: %s", stdout)

	exists, err := tc.CheckFileExists(probeLog)
	require.NoError(t, err)
	assert.False(t, exists, "completing built-in quest must not dispatch camp-app")
}
