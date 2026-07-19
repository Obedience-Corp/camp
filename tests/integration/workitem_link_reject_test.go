//go:build integration
// +build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_LinkRejectsRelatedProjectNoMutation(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/link-reject-related-project"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "someproj")
	seedDesignWorkitem(t, tc, dir, "wi")

	// Establish a baseline link so links.yaml exists and has known content.
	_, err := tc.RunCampInDir(dir, "workitem", "link", "wi", "--project", "someproj")
	require.NoError(t, err)
	before, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(dir, "workitem", "link", "wi", "--project", "someproj", "--role", "related")
	require.Error(t, err, "related+project must be rejected: %s", out)
	assert.Contains(t, out, "--project", "the error must name --project on create/adopt as the alternative")
	assert.Contains(t, out, "deprecated", "the error must explain the deprecation")

	after, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Equal(t, before, after, "a rejected link must not mutate links.yaml at all")
}

func TestIntegration_LinkPrimaryProjectStillWorks(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/link-primary-project"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "someproj")
	seedDesignWorkitem(t, tc, dir, "wi")

	out, err := tc.RunCampInDir(dir, "workitem", "link", "wi", "--project", "someproj")
	require.NoError(t, err, "primary+project (default role) must still work: %s", out)
	assert.Contains(t, out, "linked")

	linksYAML, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, linksYAML, "projects/someproj")
	assert.Contains(t, linksYAML, "role: primary")
}

func TestIntegration_LinkRelatedNonProjectStillWorks(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/link-related-nonproject"
	initLinksCampaign(t, tc, dir)
	seedDesignWorkitem(t, tc, dir, "wi")

	// related + a non-project scope: the rejection is project-scoped, so this is
	// allowed. --allow-missing skips scope-existence validation so the row writes.
	out, err := tc.RunCampInDir(dir, "workitem", "link", "wi", "--festival", "somefest", "--role", "related", "--allow-missing")
	require.NoError(t, err, "related + non-project scope must still succeed: %s", out)
	assert.Contains(t, out, "linked")

	linksYAML, err := tc.ReadFile(dir + "/.campaign/workitems/links.yaml")
	require.NoError(t, err)
	assert.Contains(t, linksYAML, "role: related")
}

func TestIntegration_LinkNonRelatedProjectStillWorks(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/link-nonrelated-project"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "someproj")
	seedDesignWorkitem(t, tc, dir, "wi")

	for _, role := range []string{"blocked_by", "supersedes"} {
		out, err := tc.RunCampInDir(dir, "workitem", "link", "wi", "--project", "someproj", "--role", role)
		require.NoError(t, err, "%s+project must still work (only related is blocked): %s", role, out)
		assert.Contains(t, out, "linked")
	}
}

func TestIntegration_LinkAllowMissingDoesNotBypassRejection(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/link-reject-allowmissing"
	initLinksCampaign(t, tc, dir)
	seedProject(t, tc, dir, "someproj")
	seedDesignWorkitem(t, tc, dir, "wi")

	out, err := tc.RunCampInDir(dir, "workitem", "link", "wi", "--project", "someproj", "--role", "related", "--allow-missing")
	require.Error(t, err, "--allow-missing must not bypass the related+project rejection: %s", out)
	assert.Contains(t, out, "--project", "the rejection error still names the alternative")
}
