//go:build integration
// +build integration

package integration

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The commit that retires or moves a workitem is the one worth finding again,
// so its campaign tag has to name the workitem. Without the WI- segment the
// only trace is the slug in the body, and nothing that matches on the tag
// (`camp workitem commits`, a git log grep on the ref) will find it.

// seedPromoteTagsCommit records the fixture so the promote under test is the
// commit at HEAD.
func seedPromoteTagsCommit(t *testing.T, tc *TestContainer, dir string) {
	t.Helper()
	tc.Shell(t, "cd "+dir+" && git add -A && git -c user.email=t@e -c user.name=t commit -q -m seed")
}

func TestIntegration_PromoteCommitTags_WorkitemPromoteCarriesRef(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/promote-tags-workitem"
	initCommitTagsCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")
	seedPromoteTagsCommit(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workitem", "promote", "timeline", "--target", "completed")
	require.NoError(t, err, "workitem promote: %s", out)

	subject := lastCommitSubject(t, tc, dir)
	assert.Contains(t, subject, ref, "promote commit must carry the WI-<ref>: %s", subject)
}

func TestIntegration_PromoteCommitTags_RailPromoteCarriesRef(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/promote-tags-rail"
	initCommitTagsCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")
	seedPromoteTagsCommit(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workitem", "promote", "timeline", "--target", "ready")
	require.NoError(t, err, "rail promote: %s", out)

	subject := lastCommitSubject(t, tc, dir)
	assert.Contains(t, subject, ref, "rail promote commit must carry the WI-<ref>: %s", subject)
}

func TestIntegration_PromoteCommitTags_DemoteCarriesRef(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/promote-tags-demote"
	initCommitTagsCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")
	seedPromoteTagsCommit(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workitem", "promote", "timeline", "--target", "ready")
	require.NoError(t, err, "rail promote: %s", out)
	out, err = tc.RunCampInDir(dir, "workitem", "demote", "timeline")
	require.NoError(t, err, "demote: %s", out)

	subject := lastCommitSubject(t, tc, dir)
	assert.Contains(t, subject, ref, "demote commit must carry the WI-<ref>: %s", subject)
}

// A marker written before refs existed still has to produce a findable commit,
// so promote backfills the ref rather than shipping an untagged retirement.
func TestIntegration_PromoteCommitTags_BackfillsMissingRefOnPromote(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/promote-tags-backfill"
	initCommitTagsCampaign(t, tc, dir)

	wiDir := dir + "/workflow/design/legacy"
	require.NoError(t, tc.WriteFile(wiDir+"/.workitem", `version: v1alpha5
kind: workitem
id: design-legacy-2026-05-25
type: design
title: legacy
`))
	require.NoError(t, tc.WriteFile(wiDir+"/README.md", "legacy\n"))
	seedPromoteTagsCommit(t, tc, dir)

	out, err := tc.RunCampInDir(dir, "workitem", "promote", "legacy", "--target", "completed")
	require.NoError(t, err, "workitem promote: %s", out)

	subject := lastCommitSubject(t, tc, dir)
	assert.Regexp(t, `-WI-[0-9a-f]{6}`, subject,
		"promote must backfill and carry a ref: %s", subject)
}

// An intent promoted from the camp root is not the workitem the operator is
// standing in, so the tag has to name the intent rather than inherit ambient
// context (or, as it did, carry no WI- segment at all).
func TestIntegration_PromoteCommitTags_IntentPromoteCarriesOwnRef(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/promote-tags-intent"
	initCommitTagsCampaign(t, tc, dir)

	// intent add auto-commits, so the capture is already recorded and the
	// promote below lands as its own commit at HEAD.
	out, err := tc.RunCampInDir(dir, "intent", "add", "Ship the timeline generator")
	require.NoError(t, err, "intent add: %s", out)
	intentID := soleIntentID(t, tc, dir+"/.campaign/intents/inbox")

	out, err = tc.RunCampInDir(dir, "intent", "promote", intentID, "--target", "ready")
	require.NoError(t, err, "intent promote: %s", out)

	subject := lastCommitSubject(t, tc, dir)
	assert.Regexp(t, `-WI-[0-9a-f]{6}`, subject,
		"intent promote must carry the intent's own ref: %s", subject)

	body, err := tc.ReadFile(dir + "/.campaign/intents/ready/" + intentID + ".md")
	require.NoError(t, err)
	assert.Contains(t, body, "ref: WI-",
		"the ref must be stamped into the intent frontmatter, got:\n%s", body)
}

// soleIntentID returns the id of the only intent markdown file in dir.
func soleIntentID(t *testing.T, tc *TestContainer, dir string) string {
	t.Helper()
	listing := tc.Shell(t, "ls "+dir)
	var ids []string
	for _, name := range strings.Fields(listing) {
		if filepath.Ext(name) == ".md" {
			ids = append(ids, strings.TrimSuffix(name, ".md"))
		}
	}
	require.Len(t, ids, 1, "expected exactly one intent in %s, got %q", dir, listing)
	return ids[0]
}
