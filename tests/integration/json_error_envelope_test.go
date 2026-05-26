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

type errEnvelope struct {
	SchemaVersion string `json:"schema_version"`
	Error         struct {
		Code     string `json:"code"`
		Message  string `json:"message"`
		Hint     string `json:"hint"`
		ExitCode int    `json:"exit_code"`
	} `json:"error"`
}

func TestIntegration_WorkitemCommit_JSONErrorEnvelope_NoContext(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/json-env-no-context"
	initCommitTagsCampaign(t, tc, dir)

	out, _, err := tc.ExecCommand("sh", "-c",
		"cd "+dir+" && /camp workitem commit -m 'no ctx' --json 2>&1; echo EXIT=$?")
	require.NoError(t, err)

	jsonStart := strings.Index(out, "{")
	require.GreaterOrEqual(t, jsonStart, 0, "no JSON envelope in: %s", out)
	jsonEnd := strings.Index(out, "EXIT=")
	require.Greater(t, jsonEnd, jsonStart, "no EXIT marker: %s", out)
	payload := strings.TrimSpace(out[jsonStart:jsonEnd])

	var env errEnvelope
	require.NoError(t, json.Unmarshal([]byte(payload), &env), "parse: %s", payload)
	assert.Equal(t, "workitem-commit/v1alpha1", env.SchemaVersion)
	assert.NotEmpty(t, env.Error.Message, "envelope must carry a message: %s", payload)
	assert.NotZero(t, env.Error.ExitCode, "exit_code must be set: %s", payload)

	assert.NotContains(t, out, "Usage:",
		"--json refusal must NOT emit cobra usage text: %s", out)
	assert.NotContains(t, out, "Flags:",
		"--json refusal must NOT emit cobra flag listing: %s", out)
	assert.Contains(t, out, "EXIT="+itoa(env.Error.ExitCode),
		"actual exit code must match envelope.exit_code: %s", out)
}

func TestIntegration_WorkitemLink_JSONErrorEnvelope_UnknownWorkitem(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/json-env-unknown"
	initLinksCampaign(t, tc, dir)

	out, _, err := tc.ExecCommand("sh", "-c",
		"cd "+dir+" && /camp workitem link ghost --project demo --role primary --json 2>&1; echo EXIT=$?")
	require.NoError(t, err)
	jsonStart := strings.Index(out, "{")
	require.GreaterOrEqual(t, jsonStart, 0, "no JSON envelope in: %s", out)
	jsonEnd := strings.Index(out, "EXIT=")
	payload := strings.TrimSpace(out[jsonStart:jsonEnd])

	var env errEnvelope
	require.NoError(t, json.Unmarshal([]byte(payload), &env), "parse: %s", payload)
	assert.Equal(t, "workitem-links/v1alpha1", env.SchemaVersion)
	assert.NotEmpty(t, env.Error.Code)
	assert.NotContains(t, out, "Usage:",
		"--json refusal must NOT emit cobra usage: %s", out)
}

// TestIntegration_WorkitemLink_JSONFalseDisablesEnvelope reproduces the
// PR #313 review repro: `--json=false` must NOT render the JSON error
// envelope. Before the Requested fix, the argv scan matched any
// `--json=` token as opt-in and surfaced a JSON payload even when the
// caller explicitly disabled it.
func TestIntegration_WorkitemLink_JSONFalseDisablesEnvelope(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/json-false-disables"
	initLinksCampaign(t, tc, dir)

	out, _, err := tc.ExecCommand("sh", "-c",
		"cd "+dir+" && /camp workitem link ghost --project demo --role primary --json=false 2>&1; echo EXIT=$?")
	require.NoError(t, err)
	assert.NotContains(t, out, `"schema_version"`,
		"--json=false must NOT render the JSON envelope, got: %s", out)
	assert.NotContains(t, out, `"error"`,
		"--json=false must NOT render the JSON error payload, got: %s", out)
	assert.Contains(t, out, "ghost",
		"human error output must still mention the missing workitem: %s", out)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
