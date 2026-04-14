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

// These tests guard the submodule path handling in the `camp project remote`
// subcommands. The underlying concern: commits 75948bf and successors
// switched `submodulePath := strings.TrimPrefix(resolved.Path, campRoot+"/")`
// to `submodulePath := resolved.LogicalPath` across list/remove/rename/set_url
// in cmd/camp/project/remote/. Both forms should produce a campaign-relative
// key (e.g. `projects/my-sub`) — if either path ever drifted toward an
// absolute path or an empty string, `.gitmodules` would get a broken
// `[submodule ".../projects/name"]` section and the submodule would become
// unusable across machines (the whole point of `.gitmodules` is portability).
//
// Before this file there was zero integration coverage for `remote set-url`
// or `remote rename` touching `.gitmodules`. These tests close that gap.

// TestProject_RemoteSetURL_SubmoduleKeyIsCampaignRelative exercises
// `camp project remote set-url` on a real submodule and asserts the key
// written into `.gitmodules` is the campaign-relative path (`projects/<name>`)
// not an absolute path or anything else.
func TestProject_RemoteSetURL_SubmoduleKeyIsCampaignRelative(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/remote-seturl-sub"
	remoteRepo := "/test/remote-seturl-origin"
	projectName := "remote-seturl-origin"
	newURL := "git@github.com:obedience-corp/renamed-submodule.git"

	_, err := tc.InitCampaign(campaignPath, "remote-seturl-sub", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(remoteRepo))

	_, err = tc.RunCampInDir(campaignPath, "project", "add", remoteRepo, "--local", remoteRepo)
	require.NoError(t, err, "adding a submodule via --local should succeed")

	// Sanity: the submodule was registered as a submodule (not a symlink)
	// so set-url should hit the .gitmodules path.
	gitmodules, err := tc.ReadFile(campaignPath + "/.gitmodules")
	require.NoError(t, err, ".gitmodules should exist after submodule add")
	expectedKey := fmt.Sprintf(`[submodule "projects/%s"]`, projectName)
	assert.Contains(t, gitmodules, expectedKey,
		".gitmodules should declare the submodule under the campaign-relative key (found: %q)", gitmodules)

	// Run set-url, skipping connectivity check (the new URL isn't reachable)
	// and skipping auto-stage so we isolate the .gitmodules write.
	output, err := tc.RunCampInDir(
		campaignPath,
		"project", "remote", "set-url", newURL,
		"--project", projectName,
		"--no-verify",
		"--no-stage",
	)
	require.NoError(t, err, "set-url should succeed:\n%s", output)
	assert.Contains(t, output, "Updated .gitmodules")

	// Verify: .gitmodules still has the campaign-relative submodule section
	// AND the URL was written into the right key.
	gitmodulesAfter, err := tc.ReadFile(campaignPath + "/.gitmodules")
	require.NoError(t, err)
	assert.Contains(t, gitmodulesAfter, expectedKey,
		".gitmodules must keep the campaign-relative section header after set-url (got: %q)", gitmodulesAfter)
	assert.Contains(t, gitmodulesAfter, "url = "+newURL,
		".gitmodules must contain the new URL")

	// Defensive: no absolute-path or empty-path section snuck in.
	assert.NotContains(t, gitmodulesAfter, "[submodule \"/",
		".gitmodules must not contain an absolute-path section (regression guard)")
	assert.NotContains(t, gitmodulesAfter, "[submodule \"\"]",
		".gitmodules must not contain an empty-name section (regression guard)")

	// Canonical verification: ask git to resolve the key directly.
	declared, _, err := tc.ExecCommand("git", "-C", campaignPath,
		"config", "-f", ".gitmodules",
		fmt.Sprintf("submodule.projects/%s.url", projectName))
	require.NoError(t, err, "git config should find the campaign-relative key")
	assert.Equal(t, newURL, strings.TrimSpace(declared),
		"git config resolution under the campaign-relative key should return the new URL")
}

// TestProject_RemoteRename_ToOriginUpdatesCampaignRelativeKey exercises the
// `camp project remote rename <other> origin` flow for submodules. This path
// re-declares .gitmodules with the origin URL and must target the
// campaign-relative key just like set-url does.
func TestProject_RemoteRename_ToOriginUpdatesCampaignRelativeKey(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/remote-rename-sub"
	remoteRepo := "/test/remote-rename-origin"
	projectName := "remote-rename-origin"
	upstreamURL := "git@github.com:obedience-corp/rename-upstream.git"

	_, err := tc.InitCampaign(campaignPath, "remote-rename-sub", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(remoteRepo))

	_, err = tc.RunCampInDir(campaignPath, "project", "add", remoteRepo, "--local", remoteRepo)
	require.NoError(t, err)

	projectDir := campaignPath + "/projects/" + projectName

	// Add a second remote named "upstream" inside the submodule. We'll rename
	// it TO origin to trigger the .gitmodules re-declare code path.
	_, _, err = tc.ExecCommand("git", "-C", projectDir, "remote", "add", "upstream", upstreamURL)
	require.NoError(t, err)

	// Remove the existing origin so `rename upstream origin` doesn't collide.
	_, _, err = tc.ExecCommand("git", "-C", projectDir, "remote", "remove", "origin")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(
		campaignPath,
		"project", "remote", "rename", "upstream", "origin",
		"--project", projectName,
	)
	require.NoError(t, err, "rename upstream→origin should succeed:\n%s", output)
	assert.Contains(t, output, "Updated .gitmodules to use "+upstreamURL)

	// Verify .gitmodules has the campaign-relative key with the upstream URL.
	gitmodulesAfter, err := tc.ReadFile(campaignPath + "/.gitmodules")
	require.NoError(t, err)
	expectedKey := fmt.Sprintf(`[submodule "projects/%s"]`, projectName)
	assert.Contains(t, gitmodulesAfter, expectedKey,
		".gitmodules must keep the campaign-relative section header after rename (got: %q)", gitmodulesAfter)
	assert.Contains(t, gitmodulesAfter, "url = "+upstreamURL,
		".gitmodules must reflect the new origin URL")

	// Canonical verification via git config.
	declared, _, err := tc.ExecCommand("git", "-C", campaignPath,
		"config", "-f", ".gitmodules",
		fmt.Sprintf("submodule.projects/%s.url", projectName))
	require.NoError(t, err)
	assert.Equal(t, upstreamURL, strings.TrimSpace(declared),
		"git config resolution under the campaign-relative key should return the renamed URL")
}
