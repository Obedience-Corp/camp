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

func TestIntegration_WorkitemCommit_SymlinkedCampaignRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	real := "/test/wi-commit-sym-real"
	link := "/test/wi-commit-sym-link"
	initWorkitemCommitCampaign(t, tc, real)
	ref := seedDesignWorkitemWithRef(t, tc, real, "timeline")

	require.NoError(t, tc.CreateGitRepo(real+"/projects/camp-timeline"))
	_, err := tc.RunCampInDir(real, "workitem", "link", "timeline", "--project", "camp-timeline")
	require.NoError(t, err)

	_, code, err := tc.ExecCommand("ln", "-s", real, link)
	require.NoError(t, err)
	require.Equal(t, 0, code)

	rootHead, _, herr := tc.ExecCommand("git", "-C", real, "rev-parse", "HEAD")
	require.NoError(t, herr)
	rootHead = strings.TrimSpace(rootHead)

	require.NoError(t, tc.WriteFile(real+"/projects/camp-timeline/foo.go", "package x\n"))
	out, err := tc.RunCampInDir(link+"/projects/camp-timeline",
		"workitem", "commit", "--workitem", "timeline", "-m", "feat: stub via symlink")
	require.NoError(t, err, "camp workitem commit via symlink: %s", out)

	subject := lastCommitSubject(t, tc, real+"/projects/camp-timeline")
	assert.Contains(t, subject, ref,
		"project subject must include WI-<ref> after symlinked-cwd commit: %s", subject)

	rootHeadAfter, _, herr := tc.ExecCommand("git", "-C", real, "rev-parse", "HEAD")
	require.NoError(t, herr)
	assert.Equal(t, rootHead, strings.TrimSpace(rootHeadAfter),
		"campaign root HEAD must not advance when commit was routed via symlinked cwd")
}

func TestIntegration_WorkitemCommit_FailureModes(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-failure"
	initWorkitemCommitCampaign(t, tc, dir)

	out, code, err := tc.ExecCommand("sh", "-c", "cd "+dir+" && /camp workitem commit -m 'no context' 2>&1")
	require.NoError(t, err)
	require.Equal(t, 2, code, "expected exit 2, output: %s", out)
	assert.Contains(t, out, "no workitem context",
		"output should explain no workitem context:\n%s", out)

	stdoutPath := "/tmp/workitem-commit-json-error-stdout"
	stderrPath := "/tmp/workitem-commit-json-error-stderr"
	_, code, err = tc.ExecCommand("sh", "-c",
		"cd "+dir+" && /camp workitem commit --json -m 'no context' >"+stdoutPath+" 2>"+stderrPath)
	require.NoError(t, err)
	require.Equal(t, 2, code)

	stdout, err := tc.ReadFile(stdoutPath)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	stderr, err := tc.ReadFile(stderrPath)
	require.NoError(t, err)
	assert.NotContains(t, stderr, "Usage:")

	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Error         struct {
			Code     string `json:"code"`
			Message  string `json:"message"`
			Hint     string `json:"hint"`
			ExitCode int    `json:"exit_code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(stderr), &envelope), "stderr=%s", stderr)
	assert.Equal(t, "workitem-commit/v1alpha1", envelope.SchemaVersion)
	assert.Equal(t, "validation_error", envelope.Error.Code)
	assert.Equal(t, "no workitem context resolved from cwd", envelope.Error.Message)
	assert.Contains(t, envelope.Error.Hint, "camp workitem link")
	assert.Equal(t, 2, envelope.Error.ExitCode)
}

func TestIntegration_WorkitemCommit_DeferredDoesNotReportPreCommitHash(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	dir := "/test/wi-commit-deferred"
	initWorkitemCommitCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/timeline/notes.md", "notes\n"))
	before := strings.TrimSpace(tc.GitOutput(t, dir, "rev-parse", "HEAD"))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(dir,
		"workitem", "commit", "timeline", "-m", "deferred commit")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	combined := stdout + stderr
	assert.Contains(t, combined, "queued",
		"deferred workitem commit must report queued work; output:\n%s", combined)
	assert.NotContains(t, combined, "committed "+before,
		"deferred workitem commit must not report the pre-commit HEAD; output:\n%s", combined)

	drainJobs(t, tc, dir)
	after := strings.TrimSpace(tc.GitOutput(t, dir, "rev-parse", "HEAD"))
	assert.NotEqual(t, before, after, "the deferred commit must land after the worker runs")

	subject := strings.TrimSpace(tc.GitOutput(t, dir, "log", "-1", "--format=%s"))
	assert.Contains(t, subject, ref, "deferred commit subject must carry the workitem ref")
	assert.Contains(t, subject, "deferred commit", "deferred commit subject must carry the message")
}

func TestIntegration_WorkitemCommit_DeferredJSONCarriesNoSHA(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	dir := "/test/wi-commit-deferred-json"
	initWorkitemCommitCampaign(t, tc, dir)
	seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/timeline/spec.md", "spec\n"))
	before := strings.TrimSpace(tc.GitOutput(t, dir, "rev-parse", "HEAD"))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(dir,
		"workitem", "commit", "timeline", "-m", "deferred json", "--json")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		SHA           string `json:"sha"`
		Deferred      bool   `json:"deferred"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &payload), "stdout=%s", stdout)
	assert.Equal(t, "workitem-commit/v1alpha1", payload.SchemaVersion)
	assert.True(t, payload.Deferred, "deferred JSON result must carry deferred=true")
	assert.Empty(t, payload.SHA, "deferred JSON result must not carry a sha")
	assert.NotEqual(t, before, payload.SHA, "deferred JSON result must not carry the pre-commit HEAD")

	drainJobs(t, tc, dir)
}
