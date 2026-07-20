//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRegister_InitializesFestivals covers the campaign `camp register` builds
// when it offers to initialize a directory that is not yet a campaign.
//
// That path delegates to the scaffolder, which used to carry its own copy of
// the fest invocation and passed a --path flag the fest CLI has never accepted.
// The call failed on every run, the failure was non-fatal by design, and
// register still printed its success line, so the command reported having set
// up a campaign that had no festivals/ directory in it.
//
// Nothing executed camp's fest invocation anywhere in the suite, which is why
// the mismatch survived. The assertion here is deliberately on the outcome
// rather than on the argv: it has to keep holding no matter which code path
// ends up owning the call.
func TestRegister_InitializesFestivals(t *testing.T) {
	if !festAvailable {
		t.Skip("fest binary not available in container; skipping festival-present test")
	}

	tc := GetSharedContainer(t)
	path := "/campaigns/register-bare-dir"
	tc.Shell(t, fmt.Sprintf("rm -rf %s && mkdir -p %s", path, path))

	// register prompts before initializing, so the confirmation is piped in.
	output := tc.Shell(t, fmt.Sprintf("printf 'y\\n' | /camp register %s --name register-bare-dir 2>&1", path))

	require.Contains(t, output, "Initialized and registered campaign",
		"register should report initializing the campaign; output: %s", output)

	exists, err := tc.CheckDirExists(path + "/festivals")
	require.NoError(t, err)
	assert.True(t, exists,
		"festivals/ should exist after register initializes a campaign; output: %s", output)

	// A festivals/ directory that fest did not populate would pass a bare
	// existence check while still being unusable, so require a fest marker.
	markers := []string{".festival", "fest.yaml", ".fest"}
	found := 0
	for _, marker := range markers {
		if ok, checkErr := tc.CheckDirExists(path + "/festivals/" + marker); checkErr == nil && ok {
			found++
			continue
		}
		if ok, checkErr := tc.CheckFileExists(path + "/festivals/" + marker); checkErr == nil && ok {
			found++
		}
	}
	assert.NotZero(t, found,
		"festivals/ should carry a fest marker (%s), meaning fest init actually ran",
		strings.Join(markers, ", "))
}

// TestInit_NoFestWarningOnStderr pins the second symptom of the same defect.
//
// `camp init` called fest twice: the scaffolder's broken invocation ran first
// and printed "Warning: fest init failed", then the command-level call ran and
// quietly succeeded. festivals/ was created either way, so the only evidence
// was a warning on stderr contradicted by the success line on stdout. Users
// reasonably read that as noise, which is the other reason this went unnoticed.
func TestInit_NoFestWarningOnStderr(t *testing.T) {
	if !festAvailable {
		t.Skip("fest binary not available in container; skipping festival-present test")
	}

	tc := GetSharedContainer(t)
	path := "/campaigns/init-clean-stderr"

	stdout, stderr, exitCode, err := tc.RunCampSplit("init", path,
		"--name", "init-clean-stderr",
		"-d", "desc",
		"-m", "mission",
		"--no-git",
	)
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "camp init should succeed; stdout: %s stderr: %s", stdout, stderr)

	assert.NotContains(t, stderr, "fest init failed",
		"camp init should not report a fest failure it then silently recovers from")
	assert.NotContains(t, stdout, "fest init failed - run manually",
		"camp init should not list festivals/ as skipped when it creates it")

	exists, checkErr := tc.CheckDirExists(path + "/festivals")
	require.NoError(t, checkErr)
	assert.True(t, exists, "festivals/ should exist when fest is available")
}
