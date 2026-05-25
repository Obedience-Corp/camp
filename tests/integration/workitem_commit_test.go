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

// initWorkitemCommitCampaign mirrors initCommitTagsCampaign with a slightly
// different campaign name so the per-test directories stay isolated.
func initWorkitemCommitCampaign(t *testing.T, tc *TestContainer, dir string) {
	t.Helper()
	_, err := tc.RunCamp(
		"init", dir,
		"--name", "Workitem Commit",
		"--type", "product",
		"-d", "Workitem commit integration",
		"-m", "Verify scoped workitem commits",
		"--force",
		"--no-register",
		"--no-git",
	)
	require.NoError(t, err, "camp init")
	require.NoError(t, tc.CreateGitRepo(dir))
}

func headChangedPaths(t *testing.T, tc *TestContainer, repo string) []string {
	t.Helper()
	out, code, err := tc.ExecCommand("git", "-C", repo, "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
	require.NoError(t, err)
	require.Equal(t, 0, code, "git diff-tree failed: %s", out)
	var paths []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		paths = append(paths, line)
	}
	return paths
}

func statusPorcelain(t *testing.T, tc *TestContainer, repo string) string {
	t.Helper()
	out, code, err := tc.ExecCommand("git", "-C", repo, "status", "--porcelain")
	require.NoError(t, err)
	require.Equal(t, 0, code, "git status failed: %s", out)
	return out
}

func TestIntegration_WorkitemCommit_WorkitemDirContext(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-dir"
	initWorkitemCommitCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/timeline/notes.md", "notes\n"))
	require.NoError(t, tc.WriteFile(dir+"/unrelated.txt", "unrelated\n"))

	out, err := tc.RunCampInDir(dir, "workitem", "commit", "timeline", "-m", "design: timeline notes")
	require.NoError(t, err, "camp workitem commit: %s", out)

	subject := lastCommitSubject(t, tc, dir)
	assert.Contains(t, subject, "WI-"+ref, "subject = %s", subject)

	changed := headChangedPaths(t, tc, dir)
	assert.Contains(t, changed, "workflow/design/timeline/notes.md")
	assert.NotContains(t, changed, "unrelated.txt", "unrelated path leaked into commit: %v", changed)

	status := statusPorcelain(t, tc, dir)
	assert.Contains(t, status, "unrelated.txt", "unrelated.txt should still be dirty: %q", status)
}

func TestIntegration_WorkitemCommit_LinkedProject(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-linked"
	initWorkitemCommitCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	require.NoError(t, tc.CreateGitRepo(dir+"/projects/camp-timeline"))
	_, err := tc.RunCampInDir(dir, "workitem", "link", "timeline", "--project", "camp-timeline")
	require.NoError(t, err)

	// Capture the campaign-root HEAD so we can prove it did not move.
	rootHead, _, herr := tc.ExecCommand("git", "-C", dir, "rev-parse", "HEAD")
	require.NoError(t, herr)
	rootHead = strings.TrimSpace(rootHead)

	require.NoError(t, tc.WriteFile(dir+"/projects/camp-timeline/foo.go", "package x\n"))
	out, err := tc.RunCampInDir(dir+"/projects/camp-timeline",
		"workitem", "commit", "--workitem", "timeline", "-m", "feat: stub")
	require.NoError(t, err, "camp workitem commit: %s", out)

	subject := lastCommitSubject(t, tc, dir+"/projects/camp-timeline")
	assert.Contains(t, subject, "WI-"+ref, "project subject = %s", subject)

	rootHeadAfter, _, herr := tc.ExecCommand("git", "-C", dir, "rev-parse", "HEAD")
	require.NoError(t, herr)
	assert.Equal(t, rootHead, strings.TrimSpace(rootHeadAfter), "campaign root HEAD must not move")
}

func TestIntegration_WorkitemCommit_RefusesSilentWiden(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-widen"
	initWorkitemCommitCampaign(t, tc, dir)
	_ = seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/timeline/spec.md", "spec\n"))
	spacePath := dir + "/workflow/design/timeline/test slug with spaces.md"
	_, code, err := tc.ExecCommand("sh", "-c", fmt.Sprintf("printf 'space\\n' > '%s'", spacePath))
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.NoError(t, tc.WriteFile(dir+"/README.md", "readme\n"))
	require.NoError(t, tc.WriteFile(dir+"/docs/extra.md", "extra\n"))

	out, err := tc.RunCampInDir(dir, "workitem", "commit", "timeline", "-m", "test widen")
	require.NoError(t, err, "camp workitem commit: %s", out)

	changed := headChangedPaths(t, tc, dir)
	for _, p := range changed {
		assert.True(t, strings.HasPrefix(p, "workflow/design/timeline/"),
			"unexpected path %q leaked past the workitem boundary; full set: %v", p, changed)
	}
	assert.Contains(t, changed, "workflow/design/timeline/test slug with spaces.md")
	status := statusPorcelain(t, tc, dir)
	assert.Contains(t, status, "README.md", "README.md should remain dirty: %q", status)
	assert.Contains(t, status, "docs/extra.md", "docs/extra.md should remain dirty: %q", status)
}

func TestIntegration_WorkitemCommit_IncludeExclude(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-include-exclude"
	initWorkitemCommitCampaign(t, tc, dir)
	_ = seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/timeline/a.md", "a\n"))
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/timeline/b.md", "b\n"))
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/timeline/c.md", "c\n"))

	out, err := tc.RunCampInDir(dir,
		"workitem", "commit", "timeline",
		"-m", "exclude b",
		"--exclude", "workflow/design/timeline/b.md",
	)
	require.NoError(t, err, "camp workitem commit --exclude: %s", out)

	changed := headChangedPaths(t, tc, dir)
	assert.Contains(t, changed, "workflow/design/timeline/a.md")
	assert.Contains(t, changed, "workflow/design/timeline/c.md")
	assert.NotContains(t, changed, "workflow/design/timeline/b.md", "excluded path leaked: %v", changed)

	// Now exercise --include with a path outside the workitem dir. The
	// remaining workitem-dir change (b.md, from the previous excluded commit)
	// should still land alongside the explicitly included docs/extra.md.
	require.NoError(t, tc.WriteFile(dir+"/docs/extra.md", "extra\n"))
	out, err = tc.RunCampInDir(dir,
		"workitem", "commit", "timeline",
		"-m", "include extra",
		"--include", "docs/extra.md",
	)
	require.NoError(t, err, "camp workitem commit --include: %s", out)

	changed = headChangedPaths(t, tc, dir)
	assert.Contains(t, changed, "docs/extra.md", "include flag failed: %v", changed)
	assert.Contains(t, changed, "workflow/design/timeline/b.md", "b.md should land alongside the include: %v", changed)
}

func TestIntegration_WorkitemCommits_CrossRepo(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commits-crossrepo"
	initWorkitemCommitCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")
	_ = ref

	// One commit on the campaign root.
	require.NoError(t, tc.WriteFile(dir+"/workflow/design/timeline/r.md", "root\n"))
	out, err := tc.RunCampInDir(dir, "workitem", "commit", "timeline", "-m", "root contribution")
	require.NoError(t, err, "root commit: %s", out)

	// Two linked project repos with one commit each.
	for _, name := range []string{"camp-A", "camp-B"} {
		repo := dir + "/projects/" + name
		require.NoError(t, tc.CreateGitRepo(repo))
		_, err := tc.RunCampInDir(dir, "workitem", "link", "timeline", "--project", name)
		require.NoError(t, err)
		require.NoError(t, tc.WriteFile(repo+"/x.go", "package x // "+name+"\n"))
		out, err := tc.RunCampInDir(repo,
			"workitem", "commit", "--workitem", "timeline",
			"-m", fmt.Sprintf("project %s contribution", name))
		require.NoError(t, err, "project %s commit: %s", name, out)
	}

	out, err = tc.RunCampInDir(dir, "workitem", "commits", "timeline")
	require.NoError(t, err, "workitem commits: %s", out)
	for _, expect := range []string{"root contribution", "project camp-A contribution", "project camp-B contribution"} {
		assert.Contains(t, out, expect, "missing %q in commits output:\n%s", expect, out)
	}

	jsonOut, err := tc.RunCampInDir(dir, "workitem", "commits", "timeline", "--json")
	require.NoError(t, err, "workitem commits --json: %s", jsonOut)
	for _, repoName := range []string{"camp-A", "camp-B"} {
		assert.Contains(t, jsonOut, "\"repo\": \""+repoName+"\"",
			"JSON missing repo field for %s:\n%s", repoName, jsonOut)
	}
}

func TestIntegration_WorkitemCommit_FailureModes(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-failure"
	initWorkitemCommitCampaign(t, tc, dir)

	// No workitem, no override; commit from campaign root must refuse with
	// exit 2 and emit the hint that names the recovery paths.
	out, code, err := tc.ExecCommand("sh", "-c", "cd "+dir+" && /camp workitem commit -m 'no context' 2>&1")
	require.NoError(t, err)
	require.Equal(t, 2, code, "expected exit 2, output: %s", out)
	assert.Contains(t, out, "no workitem context",
		"output should explain no workitem context:\n%s", out)
}
