//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the cmd-layer attachment-pin behavior introduced in PR #270
// (issue #274). The service layer is covered by host-side unit tests in
// internal/attach and internal/git, but the cmd-layer helpers
// (buildPinForPath, markerResolvesToCampaign, findPinForCwd, resolvePin) only
// run inside the actual `camp` binary and require a real campaign, real
// symlink, real .camp marker, and a real registry entry to exercise honestly.

// TestAttachmentPin_HappyPath walks the full user flow: a symlink inside the
// campaign points at an external git repo; camp attach writes the marker;
// camp pin from inside the target produces an attachment pin; camp pins
// renders the KIND column; camp go --print resolves to the absolute target;
// camp detach removes both the marker and the .git/info/exclude entry.
func TestAttachmentPin_HappyPath(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/attach-pin-happy"
	externalPath := "/test/external-repo"
	symlinkPath := campaignPath + "/ai_docs/external-repo"

	_, err := tc.InitCampaign(campaignPath, "attach-pin-happy", "product")
	require.NoError(t, err)
	require.NoError(t, tc.CreateGitRepo(externalPath))

	// User manages the symlink themselves; camp attach only writes the marker.
	tc.Shell(t, "mkdir -p "+campaignPath+"/ai_docs && ln -s "+externalPath+" "+symlinkPath)

	output, err := tc.RunCampInDir(campaignPath, "attach", symlinkPath)
	require.NoError(t, err, "camp attach should succeed: %s", output)
	assert.Contains(t, output, "Attached to campaign:")
	assert.Contains(t, output, "followed symlink")
	assert.Contains(t, output, "added .camp to .git/info/exclude")

	marker, err := tc.ReadFile(externalPath + "/.camp")
	require.NoError(t, err, "marker should exist at resolved target")
	assert.Contains(t, marker, `"kind": "attachment"`)
	assert.Contains(t, marker, `"version": 3`)

	exclude, err := tc.ReadFile(externalPath + "/.git/info/exclude")
	require.NoError(t, err)
	assert.Contains(t, exclude, ".camp", "git exclude should list .camp after attach")

	pinOut, err := tc.RunCampInDir(externalPath, "pin", "smoke")
	require.NoError(t, err, "camp pin from attached external dir should succeed: %s", pinOut)
	assert.Contains(t, pinOut, "(attachment)")

	listOut, err := tc.RunCampInDir(campaignPath, "pins")
	require.NoError(t, err)
	assert.Contains(t, listOut, "smoke")
	assert.Contains(t, listOut, "attachment", "camp pins should render KIND=attachment for the new pin")

	resolved, err := tc.RunCampInDir(campaignPath, "go", "--print", "smoke")
	require.NoError(t, err, "camp go --print should resolve attachment pin: %s", resolved)
	assert.Contains(t, strings.TrimSpace(resolved), externalPath,
		"camp go --print should return the attachment AbsPath, not a campaign-relative join")

	_, err = tc.RunCampInDir(campaignPath, "unpin", "smoke")
	require.NoError(t, err)

	detachOut, err := tc.RunCampInDir(campaignPath, "detach", symlinkPath)
	require.NoError(t, err, "camp detach should succeed: %s", detachOut)
	assert.Contains(t, detachOut, "removed .camp from .git/info/exclude")

	exists, err := tc.CheckFileExists(externalPath + "/.camp")
	require.NoError(t, err)
	assert.False(t, exists, ".camp marker should be removed by detach")

	excludeAfter, err := tc.ReadFile(externalPath + "/.git/info/exclude")
	require.NoError(t, err)
	assert.NotContains(t, excludeAfter, ".camp",
		".git/info/exclude should no longer list .camp after detach")
}

// TestAttachmentPin_RejectsExternalWithoutMarker pins the cmd-layer rejection
// path: a pin path that resolves outside the campaign AND has no attachment
// marker on its walk-up must be refused with a hint pointing at camp attach.
// The campaign context is provided by cwd (running from inside the campaign)
// so the marker-walk codepath in markerResolvesToCampaign actually runs —
// CAMP_ROOT is a hard override that would bypass that walk entirely.
func TestAttachmentPin_RejectsExternalWithoutMarker(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/attach-pin-reject-unattached"
	externalPath := "/test/unattached-dir"

	_, err := tc.InitCampaign(campaignPath, "attach-pin-reject-unattached", "product")
	require.NoError(t, err)
	tc.Shell(t, "mkdir -p "+externalPath)

	output, err := tc.RunCampInDir(campaignPath, "pin", "should-fail", externalPath)
	require.Error(t, err, "pin of unattached external path should fail; got output: %s", output)
	assert.Contains(t, output, "outside campaign root",
		"error should mention outside-campaign-root")
	assert.Contains(t, output, "camp attach",
		"error hint should point user at camp attach")
}

// TestAttachmentPin_RejectsAttachInsideCampaign pins the in-campaign refusal
// in the cmd-layer for the same-campaign case.
func TestAttachmentPin_RejectsAttachInsideCampaign(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/attach-reject-inside"

	_, err := tc.InitCampaign(campaignPath, "attach-reject-inside", "product")
	require.NoError(t, err)
	tc.Shell(t, "mkdir -p "+campaignPath+"/docs")

	output, exitCode, err := tc.ExecCommand("sh", "-c",
		"cd "+campaignPath+" && /camp attach "+campaignPath+"/docs 2>&1")
	require.NoError(t, err)
	require.NotEqual(t, 0, exitCode, "attach inside campaign tree should fail")
	assert.Contains(t, output, "already inside campaign root",
		"error should explain why the path was rejected")
}

// TestAttachmentPin_RejectsAttachInsideOtherCampaign covers the security-
// relevant case: writing a .camp marker inside ANOTHER campaign would shadow
// that campaign's .campaign/ during detection. The any-campaign rejection
// (the fix obey-agent flagged in PR #270 review) must refuse this from the
// cmd layer, not just the service layer.
func TestAttachmentPin_RejectsAttachInsideOtherCampaign(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignA := "/campaigns/attach-cross-a"
	campaignB := "/campaigns/attach-cross-b"

	_, err := tc.InitCampaign(campaignA, "attach-cross-a", "product")
	require.NoError(t, err)
	_, err = tc.InitCampaign(campaignB, "attach-cross-b", "product")
	require.NoError(t, err)

	insideB := campaignB + "/festivals"

	output, exitCode, err := tc.ExecCommand("sh", "-c",
		"cd "+campaignA+" && /camp attach "+insideB+" 2>&1")
	require.NoError(t, err)
	require.NotEqual(t, 0, exitCode, "attach into another campaign should fail")
	assert.True(t,
		strings.Contains(output, "already inside a different campaign") ||
			strings.Contains(output, "already inside campaign root"),
		"error should refuse the cross-campaign attach; got: %s", output)

	exists, err := tc.CheckFileExists(insideB + "/.camp")
	require.NoError(t, err)
	assert.False(t, exists,
		"no .camp marker should be written inside the other campaign on failure")
}

// TestAttachmentPin_UnpinFromAttachedCwd pins the dual lookup in
// findPinForCwd: running `camp unpin` with no arguments while inside an
// attached external dir should locate the pin by AbsPath, not by relative
// path under the campaign root.
func TestAttachmentPin_UnpinFromAttachedCwd(t *testing.T) {
	tc := GetSharedContainer(t)
	campaignPath := "/campaigns/attach-pin-unpin-cwd"
	externalPath := "/test/unpin-cwd-target"
	symlinkPath := campaignPath + "/ai_docs/unpin-cwd-target"

	_, err := tc.InitCampaign(campaignPath, "attach-pin-unpin-cwd", "product")
	require.NoError(t, err)
	tc.Shell(t, "mkdir -p "+externalPath+" && mkdir -p "+campaignPath+"/ai_docs && ln -s "+externalPath+" "+symlinkPath)

	_, err = tc.RunCampInDir(campaignPath, "attach", symlinkPath)
	require.NoError(t, err)

	_, err = tc.RunCampInDir(externalPath, "pin", "cwd-target")
	require.NoError(t, err)

	output, err := tc.RunCampInDir(externalPath, "unpin")
	require.NoError(t, err,
		"camp unpin with no args from attached cwd should find the pin: %s", output)
	assert.Contains(t, output, "cwd-target", "unpin output should name the pin it removed")

	listOut, err := tc.RunCampInDir(campaignPath, "pins")
	require.NoError(t, err)
	assert.NotContains(t, listOut, "cwd-target", "pin should be gone from listing")
}
