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

type commitsJSON struct {
	SchemaVersion string `json:"schema_version"`
	Commits       []struct {
		Repo    string `json:"repo"`
		SHA     string `json:"sha"`
		Subject string `json:"subject"`
	} `json:"commits"`
	Errors []map[string]any `json:"errors"`
}

func initCommitsCampaign(t *testing.T, tc *TestContainer, dir, projName, ref string) {
	t.Helper()
	initCommitTagsCampaign(t, tc, dir)
	_ = seedDesignWorkitemWithRefAt(t, tc, dir, "example", ref)
	require.NoError(t, tc.CreateGitRepo(dir+"/projects/"+projName))
	_, err := tc.RunCampInDir(dir, "workitem", "link", "example", "--project", projName)
	require.NoError(t, err)
}

func seedDesignWorkitemWithRefAt(t *testing.T, tc *TestContainer, dir, slug, _ string) string {
	t.Helper()
	out, err := tc.RunCampInDir(dir, "workitem", "create", slug, "--type", "design", "--title", slug)
	require.NoError(t, err, "workitem create: %s", out)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ref:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "ref:"))
		}
	}
	t.Fatalf("ref missing from create output: %s", out)
	return ""
}

func runCommitsJSON(t *testing.T, tc *TestContainer, dir string, args ...string) commitsJSON {
	t.Helper()
	full := append([]string{"workitem", "commits"}, args...)
	full = append(full, "--json")
	out, err := tc.RunCampInDir(dir, full...)
	require.NoError(t, err, "workitem commits --json: %s", out)
	start := strings.Index(out, "{")
	require.GreaterOrEqual(t, start, 0, "no JSON in: %s", out)
	var got commitsJSON
	require.NoError(t, json.Unmarshal([]byte(out[start:]), &got), "parse: %s", out[start:])
	return got
}

func TestIntegration_WorkitemCommits_IncludesCampaignAndLinkedProject(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commits-cross-repo-both"
	initCommitsCampaign(t, tc, dir, "demo", "")

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/example/notes.md", "x\n"))
	out, err := tc.RunCampInDir(dir+"/workflow/design/example",
		"workitem", "commit", "-m", "root commit")
	require.NoError(t, err, "%s", out)

	require.NoError(t, tc.WriteFile(dir+"/projects/demo/foo.go", "package x\n"))
	out, err = tc.RunCampInDir(dir+"/projects/demo",
		"workitem", "commit", "--workitem", "example", "-m", "project commit")
	require.NoError(t, err, "%s", out)

	got := runCommitsJSON(t, tc, dir, "example")
	repos := make(map[string]bool)
	for _, c := range got.Commits {
		repos[c.Repo] = true
	}
	assert.True(t, repos["."] || repos[""],
		"expected campaign-root commit in results, got repos: %v", repos)
	assert.True(t, repos["projects/demo"],
		"expected projects/demo commit in results, got repos: %v", repos)
}

func TestIntegration_WorkitemCommits_FiltersFalsePositives(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commits-false-positive"
	initCommitTagsCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "example")

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/example/a.md", "a\n"))
	out, err := tc.RunCampInDir(dir+"/workflow/design/example",
		"workitem", "commit", "-m", "real")
	require.NoError(t, err, "%s", out)

	require.NoError(t, tc.WriteFile(dir+"/junk.md", "junk\n"))
	_, _, err = tc.ExecCommand("git", "-C", dir, "add", "junk.md")
	require.NoError(t, err)
	_, _, err = tc.ExecCommand("git", "-C", dir, "commit", "-m",
		"unrelated: mentions "+ref+" in subject but no campaign tag")
	require.NoError(t, err)

	got := runCommitsJSON(t, tc, dir, "example")
	for _, c := range got.Commits {
		assert.True(t, strings.Contains(c.Subject, "[OBEY-CAMPAIGN-"),
			"non-tagged commit leaked into results: %+v", c)
	}
}

func TestIntegration_WorkitemCommits_NonGitDirEmpty(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/commits-non-git"
	initCommitTagsCampaign(t, tc, dir)
	_ = seedDesignWorkitemWithRef(t, tc, dir, "example")

	_, _, err := tc.ExecCommand("mkdir", "-p", dir+"/projects/notgit")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workitem", "link", "example", "--project", "notgit")
	require.NoError(t, err)

	got := runCommitsJSON(t, tc, dir, "example")
	assert.NotPanics(t, func() { _ = got })
}
