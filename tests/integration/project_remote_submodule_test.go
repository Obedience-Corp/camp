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

func TestProject_RemoteSetURL_UsesDeclaredSectionName(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/remote-seturl-named"
	remoteRepo := "/test/remote-seturl-named-origin"
	projectName := "remote-seturl-named-origin"
	projectPath := campaignPath + "/projects/" + projectName
	sectionName := "legacy-short-name"
	newURL := "git@github.com:obedience-corp/renamed-submodule.git"

	_, err := tc.InitCampaign(campaignPath, "remote-seturl-named", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(remoteRepo))
	_, err = tc.RunCampInDir(campaignPath, "project", "add", remoteRepo, "--local", remoteRepo)
	require.NoError(t, err)

	oldSection := "submodule.projects/" + projectName
	newSection := "submodule." + sectionName
	_, _, err = tc.ExecCommand("git", "-C", campaignPath, "config", "-f", ".gitmodules", "--rename-section", oldSection, newSection)
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("git", "-C", campaignPath, "config", "--rename-section", oldSection, newSection)
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "project", "remote", "set-url", newURL,
		"--project", projectName, "--no-verify", "--no-stage")
	require.NoError(t, err, "set-url should update the existing section:\n%s", output)

	gitmodules, err := tc.ReadFile(campaignPath + "/.gitmodules")
	require.NoError(t, err)
	assert.Contains(t, gitmodules, `[submodule "`+sectionName+`"]`)
	assert.NotContains(t, gitmodules, `[submodule "projects/`+projectName+`"]`)
	declared, _, err := tc.ExecCommand("git", "-C", campaignPath,
		"config", "-f", ".gitmodules", "--get", newSection+".url")
	require.NoError(t, err)
	assert.Equal(t, newURL, strings.TrimSpace(declared))
	active, _, err := tc.ExecCommand("git", "-C", campaignPath,
		"config", "--get", newSection+".url")
	require.NoError(t, err)
	assert.Equal(t, newURL, strings.TrimSpace(active))
	remoteURL, _, err := tc.ExecCommand("git", "-C", projectPath,
		"remote", "get-url", "origin")
	require.NoError(t, err)
	assert.Equal(t, newURL, strings.TrimSpace(remoteURL))
	listOutput, err := tc.RunCampInDir(campaignPath, "project", "remote", "list", "--project", projectName)
	require.NoError(t, err)
	assert.Contains(t, listOutput, "ok", "list should compare the legacy section's declared and active URLs")

	// A later failure must restore the same legacy section and synchronized URL.
	rollbackURL := "git@github.com:obedience-corp/rollback-test.git"
	output, err = tc.RunCampInDir(campaignPath, "project", "remote", "set-url", rollbackURL,
		"--project", projectName, "--name", "missing-remote", "--no-verify", "--no-stage")
	require.Error(t, err, "updating a nonexistent remote should trigger rollback: %s", output)
	gitmodules, err = tc.ReadFile(campaignPath + "/.gitmodules")
	require.NoError(t, err)
	assert.NotContains(t, gitmodules, rollbackURL)
	assert.NotContains(t, gitmodules, `[submodule "projects/`+projectName+`"]`)
	declared, _, err = tc.ExecCommand("git", "-C", campaignPath,
		"config", "-f", ".gitmodules", "--get", newSection+".url")
	require.NoError(t, err)
	assert.Equal(t, newURL, strings.TrimSpace(declared))
	active, _, err = tc.ExecCommand("git", "-C", campaignPath,
		"config", "--get", newSection+".url")
	require.NoError(t, err)
	assert.Equal(t, newURL, strings.TrimSpace(active))
}

func TestProject_RemoteSetURL_RejectsMissingOrAmbiguousDeclaration(t *testing.T) {
	for _, test := range []struct {
		name      string
		wantError string
	}{
		{
			name:      "missing path",
			wantError: "not found in .gitmodules",
		},
		{
			name:      "ambiguous path",
			wantError: "ambiguous .gitmodules sections",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			tc := GetSharedContainer(t)
			suffix := strings.ReplaceAll(test.name, " ", "-")
			campaignPath := "/campaigns/remote-seturl-" + suffix
			remoteRepo := "/test/remote-seturl-" + suffix + "-origin"
			projectName := "remote-seturl-" + suffix + "-origin"
			projectPath := campaignPath + "/projects/" + projectName
			logicalPath := "projects/" + projectName
			newURL := "git@github.com:obedience-corp/should-not-be-written.git"

			_, err := tc.InitCampaign(campaignPath, "remote-seturl-"+suffix, "product")
			require.NoError(t, err)
			require.NoError(t, tc.CreateGitRepo(remoteRepo))
			_, err = tc.RunCampInDir(campaignPath, "project", "add", remoteRepo, "--local", remoteRepo)
			require.NoError(t, err)

			key := "submodule." + logicalPath + ".path"
			if test.name == "missing path" {
				_, _, err = tc.ExecCommand("git", "-C", campaignPath, "config", "-f", ".gitmodules", key, "projects/other")
			} else {
				_, _, err = tc.ExecCommand("git", "-C", campaignPath, "config", "-f", ".gitmodules", "submodule.duplicate.path", logicalPath)
			}
			require.NoError(t, err)

			before, err := tc.ReadFile(campaignPath + "/.gitmodules")
			require.NoError(t, err)
			remoteBefore, _, err := tc.ExecCommand("git", "-C", projectPath, "remote", "get-url", "origin")
			require.NoError(t, err)
			localKey := "submodule." + logicalPath + ".url"
			localBefore, _, err := tc.ExecCommand("git", "-C", campaignPath, "config", "--get", localKey)
			require.NoError(t, err)

			listOutput, err := tc.RunCampInDir(campaignPath, "project", "remote", "list", "--project", projectName)
			require.Error(t, err, "list must report an invalid declaration instead of omitting status")
			assert.Contains(t, listOutput, test.wantError)

			output, err := tc.RunCampInDir(campaignPath, "project", "remote", "set-url", newURL,
				"--project", projectName, "--no-verify", "--no-stage")
			require.Error(t, err)
			assert.Contains(t, output, test.wantError)
			after, err := tc.ReadFile(campaignPath + "/.gitmodules")
			require.NoError(t, err)
			assert.Equal(t, before, after, "failed preflight must not rewrite .gitmodules")
			remoteAfter, _, err := tc.ExecCommand("git", "-C", projectPath, "remote", "get-url", "origin")
			require.NoError(t, err)
			assert.Equal(t, remoteBefore, remoteAfter, "failed preflight must not rewrite origin")
			localAfter, _, err := tc.ExecCommand("git", "-C", campaignPath, "config", "--get", localKey)
			require.NoError(t, err)
			assert.Equal(t, localBefore, localAfter, "failed preflight must not rewrite local submodule config")
		})
	}
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

func TestProject_RemoteRename_UsesDeclaredSectionName(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/remote-rename-named"
	remoteRepo := "/test/remote-rename-named-origin"
	projectName := "remote-rename-named-origin"
	projectPath := campaignPath + "/projects/" + projectName
	newURL := "git@github.com:obedience-corp/renamed-origin.git"

	_, err := tc.InitCampaign(campaignPath, "remote-rename-named", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(remoteRepo))
	_, err = tc.RunCampInDir(campaignPath, "project", "add", remoteRepo, "--local", remoteRepo)
	require.NoError(t, err)
	oldSection := "submodule.projects/" + projectName
	newSection := "submodule.legacy-rename"
	_, _, err = tc.ExecCommand("git", "-C", campaignPath, "config", "-f", ".gitmodules", "--rename-section", oldSection, newSection)
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("git", "-C", campaignPath, "config", "--rename-section", oldSection, newSection)
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("git", "-C", projectPath, "remote", "add", "upstream", newURL)
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("git", "-C", projectPath, "remote", "remove", "origin")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "project", "remote", "rename", "upstream", "origin", "--project", projectName)
	require.NoError(t, err, "rename should update legacy section: %s", output)
	declared, _, err := tc.ExecCommand("git", "-C", campaignPath,
		"config", "-f", ".gitmodules", "--get", newSection+".url")
	require.NoError(t, err)
	assert.Equal(t, newURL, strings.TrimSpace(declared))
	gitmodules, err := tc.ReadFile(campaignPath + "/.gitmodules")
	require.NoError(t, err)
	assert.NotContains(t, gitmodules, `[submodule "projects/`+projectName+`"]`)
}

func TestProject_RemoteRemove_UsesDeclaredSectionName(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/remote-remove-named"
	remoteRepo := "/test/remote-remove-named-origin"
	projectName := "remote-remove-named-origin"
	projectPath := campaignPath + "/projects/" + projectName

	_, err := tc.InitCampaign(campaignPath, "remote-remove-named", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(remoteRepo))
	_, err = tc.RunCampInDir(campaignPath, "project", "add", remoteRepo, "--local", remoteRepo)
	require.NoError(t, err)
	oldSection := "submodule.projects/" + projectName
	_, _, err = tc.ExecCommand("git", "-C", campaignPath, "config", "-f", ".gitmodules", "--rename-section", oldSection, "submodule.legacy-remove")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(campaignPath, "project", "remote", "remove", "origin", "--force", "--project", projectName)
	require.NoError(t, err, "forced remove should clean legacy section: %s", output)
	gitmodules, err := tc.ReadFile(campaignPath + "/.gitmodules")
	require.NoError(t, err)
	assert.NotContains(t, gitmodules, `[submodule "legacy-remove"]`)
	assert.NotContains(t, gitmodules, `[submodule "projects/`+projectName+`"]`)
	_, exitCode, err := tc.ExecCommand("git", "-C", projectPath, "remote", "get-url", "origin")
	require.NoError(t, err)
	assert.NotZero(t, exitCode, "origin should be removed")
}

func TestProject_RemoteMutation_RejectsMissingOrAmbiguousDeclaration(t *testing.T) {
	for _, operation := range []string{"rename", "remove"} {
		for _, declaration := range []string{"missing", "ambiguous"} {
			t.Run(operation+"/"+declaration, func(t *testing.T) {
				tc := GetSharedContainer(t)
				suffix := operation + "-" + declaration
				campaignPath := "/campaigns/remote-" + suffix
				remoteRepo := "/test/remote-" + suffix + "-origin"
				projectName := "remote-" + suffix + "-origin"
				projectPath := campaignPath + "/projects/" + projectName
				logicalPath := "projects/" + projectName

				_, err := tc.InitCampaign(campaignPath, "remote-"+suffix, "product")
				require.NoError(t, err)
				require.NoError(t, tc.CreateGitRepo(remoteRepo))
				_, err = tc.RunCampInDir(campaignPath, "project", "add", remoteRepo, "--local", remoteRepo)
				require.NoError(t, err)
				if declaration == "missing" {
					_, _, err = tc.ExecCommand("git", "-C", campaignPath, "config", "-f", ".gitmodules",
						"submodule."+logicalPath+".path", "projects/other")
				} else {
					_, _, err = tc.ExecCommand("git", "-C", campaignPath, "config", "-f", ".gitmodules",
						"submodule.duplicate.path", logicalPath)
				}
				require.NoError(t, err)
				before, err := tc.ReadFile(campaignPath + "/.gitmodules")
				require.NoError(t, err)

				var args []string
				remoteToKeep := "origin"
				if operation == "rename" {
					remoteToKeep = "upstream"
					_, _, err = tc.ExecCommand("git", "-C", projectPath, "remote", "add", "upstream", remoteRepo)
					require.NoError(t, err)
					_, _, err = tc.ExecCommand("git", "-C", projectPath, "remote", "remove", "origin")
					require.NoError(t, err)
					args = []string{"project", "remote", "rename", "upstream", "origin", "--project", projectName}
				} else {
					args = []string{"project", "remote", "remove", "origin", "--force", "--project", projectName}
				}
				remoteBefore, _, err := tc.ExecCommand("git", "-C", projectPath, "remote", "get-url", remoteToKeep)
				require.NoError(t, err)

				output, err := tc.RunCampInDir(campaignPath, args...)
				require.Error(t, err, "invalid declaration must reject %s before mutation: %s", operation, output)
				if declaration == "missing" {
					assert.Contains(t, output, "not found in .gitmodules")
				} else {
					assert.Contains(t, output, "ambiguous .gitmodules sections")
				}
				after, err := tc.ReadFile(campaignPath + "/.gitmodules")
				require.NoError(t, err)
				assert.Equal(t, before, after)
				remoteAfter, _, err := tc.ExecCommand("git", "-C", projectPath, "remote", "get-url", remoteToKeep)
				require.NoError(t, err)
				assert.Equal(t, remoteBefore, remoteAfter, "remote must survive rejected %s", operation)
			})
		}
	}
}
