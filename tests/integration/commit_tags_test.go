//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func initCommitTagsCampaign(t *testing.T, tc *TestContainer, dir string) {
	t.Helper()
	_, err := tc.RunCamp(
		"init", dir,
		"--name", "Commit Tags",
		"--type", "product",
		"-d", "Commit tags integration",
		"-m", "Verify commit tag composition",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init")
	// Make the campaign root a git repo so commits actually run.
	require.NoError(t, tc.CreateGitRepo(dir))
}

func seedDesignWorkitemWithRef(t *testing.T, tc *TestContainer, dir, slug string) string {
	t.Helper()
	out, err := tc.RunCampInDir(dir, "workitem", "create", slug, "--type", "design", "--title", slug)
	require.NoError(t, err, "workitem create: %s", out)
	// Pull the ref out of the create output: "  ref: WI-<6 hex>".
	var ref string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ref:") {
			ref = strings.TrimSpace(strings.TrimPrefix(line, "ref:"))
			break
		}
	}
	require.NotEmpty(t, ref, "ref missing from create output: %s", out)
	return ref
}

func lastCommitSubject(t *testing.T, tc *TestContainer, repoPath string) string {
	t.Helper()
	out, code, err := tc.ExecCommand("git", "-C", repoPath, "log", "-1", "--pretty=%s")
	require.NoError(t, err)
	require.Equal(t, 0, code, "git log failed: %s", out)
	return strings.TrimSpace(out)
}

func TestIntegration_CommitTags_CampCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commit-tags-camp"
	initCommitTagsCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	// Stage the workitem dir creation into git so there is something to commit.
	_, _, err := tc.ExecCommand("git", "-C", dir, "add", "-A")
	require.NoError(t, err)

	wiDir := dir + "/workflow/design/timeline"
	out, err := tc.RunCampInDir(wiDir, "commit", "-m", "design: timeline contract")
	require.NoError(t, err, "camp commit: %s", out)

	subject := lastCommitSubject(t, tc, dir)
	assert.Contains(t, subject, "WI-"+ref, "subject should include WI-<ref>: %s", subject)
	assert.Contains(t, subject, "design: timeline contract", "subject = %s", subject)
}

func TestIntegration_CommitTags_CampPCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commit-tags-p"
	initCommitTagsCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	// Create a project repo and link the workitem to it.
	require.NoError(t, tc.CreateGitRepo(dir+"/projects/camp-timeline"))
	_, err := tc.RunCampInDir(dir, "workitem", "link", "timeline", "--project", "camp-timeline")
	require.NoError(t, err)

	// Touch a file in the project and commit via camp p commit.
	require.NoError(t, tc.WriteFile(dir+"/projects/camp-timeline/foo.go", "package x\n"))
	out, err := tc.RunCampInDir(dir+"/projects/camp-timeline",
		"p", "commit", "-m", "feat: stub")
	require.NoError(t, err, "camp p commit: %s", out)

	subject := lastCommitSubject(t, tc, dir+"/projects/camp-timeline")
	assert.Contains(t, subject, "WI-"+ref, "subject should include WI-<ref>: %s", subject)
	assert.Contains(t, subject, "feat: stub", "subject = %s", subject)
}

func TestIntegration_CommitTags_NoContext(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commit-tags-no-context"
	initCommitTagsCampaign(t, tc, dir)
	// No workitems, no links — commit from a docs/ dir should produce a tag
	// without any WI- segment.
	require.NoError(t, tc.WriteFile(dir+"/docs/notes.md", "hi\n"))
	_, _, err := tc.ExecCommand("git", "-C", dir, "add", "-A")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(dir+"/docs", "commit", "-m", "chore: cleanup")
	require.NoError(t, err, "camp commit: %s", out)

	subject := lastCommitSubject(t, tc, dir)
	assert.NotContains(t, subject, "WI-", "no-context commit must not include WI-: %s", subject)
	assert.NotContains(t, subject, "qst_", "no-context commit must not include qst_: %s", subject)
	assert.Regexp(t, `^\[OBEY-CAMPAIGN-[0-9a-f]{1,8}\]`, subject,
		"subject should still carry the campaign tag: %s", subject)
}

func TestIntegration_CommitTags_ExplicitOverride(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commit-tags-override"
	initCommitTagsCampaign(t, tc, dir)
	_ = seedDesignWorkitemWithRef(t, tc, dir, "ambient")
	ref := seedDesignWorkitemWithRef(t, tc, dir, "override-target")

	// Commit from a path NOT under either workitem; pass --workitem explicitly.
	require.NoError(t, tc.WriteFile(dir+"/docs/r.md", "explicit\n"))
	_, _, err := tc.ExecCommand("git", "-C", dir, "add", "-A")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(dir+"/docs",
		"commit", "-m", "doc: explicit", "--workitem", "override-target")
	require.NoError(t, err, "camp commit --workitem: %s", out)

	subject := lastCommitSubject(t, tc, dir)
	assert.Contains(t, subject, "WI-"+ref, "explicit override should win: %s", subject)
}

func TestIntegration_CommitTags_RejectsInvalidRef(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commit-tags-bad-ref"
	initCommitTagsCampaign(t, tc, dir)

	wiDir := dir + "/workflow/design/badref"
	marker := `version: v1alpha6
kind: workitem
id: design-badref-2026-05-25
type: design
title: badref
ref: NOT-A-VALID-REF-12345
`
	require.NoError(t, tc.WriteFile(wiDir+"/.workitem", marker))
	require.NoError(t, tc.WriteFile(wiDir+"/notes.md", "x\n"))
	_, _, err := tc.ExecCommand("git", "-C", dir, "add", "-A")
	require.NoError(t, err)

	out, _ := tc.RunCampInDir(wiDir, "commit", "-m", "design: bad ref")
	subject := lastCommitSubject(t, tc, dir)

	assert.NotContains(t, subject, "NOT-A-VALID-REF-12345",
		"commit subject must not echo hand-edited junk ref (CW0003-format-02): subject=%s out=%s",
		subject, out)
	assert.NotContains(t, subject, "WI-WI-",
		"composer must not produce doubled WI-WI- segment: subject=%s out=%s",
		subject, out)
	assert.NotContains(t, out, "WI-NOT-A-VALID-REF-12345",
		"output must not echo hand-edited junk ref: %s", out)
}

func TestIntegration_AutoWriteEnv(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commit-tags-autowrite"
	initCommitTagsCampaign(t, tc, dir)
	_ = seedDesignWorkitemWithRef(t, tc, dir, "envtest")

	// Drop a tiny hook script that dumps env and prints a fixed message.
	// Using a script keeps the campaign.yaml YAML simple (no nested quoting).
	require.NoError(t, tc.WriteFile("/tmp/commit_hook.sh",
		"#!/bin/sh\nenv | grep '^CAMP_WORKITEM_' > /tmp/env_dump\necho 'auto: from hook'\n"))
	_, _, scriptErr := tc.ExecCommand("chmod", "+x", "/tmp/commit_hook.sh")
	require.NoError(t, scriptErr)

	// Append the hooks section to the existing campaign.yaml so other
	// required fields (id, name, type, etc.) are not clobbered.
	hookYAML := "\nhooks:\n  commit_message:\n    command: /tmp/commit_hook.sh\n"
	_, _, hookErr := tc.ExecCommand("sh", "-c",
		"cat >> "+dir+"/.campaign/campaign.yaml <<EOF"+hookYAML+"EOF")
	require.NoError(t, hookErr)
	_, _, addErr := tc.ExecCommand("git", "-C", dir, "add", "-A")
	require.NoError(t, addErr)

	wiDir := dir + "/workflow/design/envtest"
	out, err := tc.RunCampInDir(wiDir, "commit", "--auto-write")
	require.NoError(t, err, "camp commit --auto-write: %s", out)

	dump, err := tc.ReadFile("/tmp/env_dump")
	require.NoError(t, err, "env dump should exist")
	for _, key := range []string{
		"CAMP_WORKITEM_ID=",
		"CAMP_WORKITEM_REF=WI-",
		"CAMP_WORKITEM_TYPE=design",
		"CAMP_WORKITEM_TITLE=envtest",
		"CAMP_WORKITEM_PATH=workflow/design/envtest",
	} {
		assert.Contains(t, dump, key, "env dump missing %s:\n%s", key, dump)
	}

	subject := lastCommitSubject(t, tc, dir)
	assert.Contains(t, subject, "auto: from hook", "commit subject should come from hook: %s", subject)
}

// fest commit and fest-side workitem resolution are deferred until camp is
// re-tagged with commitkit.PrependContextTagsFull and
// AutoWriteCommitMessageWithEnv. Skipped tests stay in tree so the contract
// reappears once the dependency lands.

func TestIntegration_CommitTags_FestCommit(t *testing.T) {
	t.Skip("fest-side wiring deferred: requires fest go.mod bump to a camp release containing commitkit.PrependContextTagsFull")
}

func TestIntegration_CommitTags_QuestAndWorkitem(t *testing.T) {
	t.Skip("quest context capture is camp-side, but the assertion currently requires the fest commit path; revisit after the camp release")
}
