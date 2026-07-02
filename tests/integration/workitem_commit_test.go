//go:build integration
// +build integration

package integration

import (
	"encoding/json"
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

func hasSubjectSuffix(subjects map[string]bool, suffix string) bool {
	for subject := range subjects {
		if strings.HasSuffix(subject, suffix) {
			return true
		}
	}
	return false
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
	assert.Contains(t, subject, ref, "subject = %s", subject)

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
	assert.Contains(t, subject, ref, "project subject = %s", subject)

	rootHeadAfter, _, herr := tc.ExecCommand("git", "-C", dir, "rev-parse", "HEAD")
	require.NoError(t, herr)
	assert.Equal(t, rootHead, strings.TrimSpace(rootHeadAfter), "campaign root HEAD must not move")
}

func TestIntegration_WorkitemCommit_FestivalContextFromCwd(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-festival"
	initWorkitemCommitCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	festDir := dir + "/festivals/active/CT0001"
	require.NoError(t, tc.WriteFile(festDir+"/FESTIVAL_GOAL.md", "# CT0001\n"))
	_, err := tc.RunCampInDir(dir, "workitem", "link", "timeline", "--festival", "CT0001")
	require.NoError(t, err)

	dryRun, err := tc.RunCampInDir(festDir,
		"workitem", "commit", "--dry-run", "--json", "-m", "festival plan")
	require.NoError(t, err, "festival dry-run: %s", dryRun)
	var plan struct {
		Context     string   `json:"context"`
		FestivalRef string   `json:"festival_ref"`
		Tag         string   `json:"tag"`
		Stage       []string `json:"stage"`
	}
	require.NoError(t, json.Unmarshal([]byte(dryRun), &plan), "raw=%s", dryRun)
	assert.Equal(t, "festival", plan.Context)
	assert.Equal(t, "CT0001", plan.FestivalRef)
	assert.Contains(t, plan.Tag, "FE-CT0001")
	assert.Contains(t, plan.Tag, ref)
	assert.Contains(t, plan.Stage, "festivals/active/CT0001/FESTIVAL_GOAL.md")

	out, err := tc.RunCampInDir(festDir,
		"workitem", "commit", "-m", "feat: festival update")
	require.NoError(t, err, "festival commit: %s", out)

	subject := lastCommitSubject(t, tc, dir)
	assert.Contains(t, subject, "FE-CT0001", "subject = %s", subject)
	assert.Contains(t, subject, ref, "subject = %s", subject)
	changed := headChangedPaths(t, tc, dir)
	assert.Contains(t, changed, "festivals/active/CT0001/FESTIVAL_GOAL.md")
	assert.Contains(t, changed, ".campaign/workitems/links.yaml")
	assert.NotContains(t, changed, "workflow/design/timeline/.workitem",
		"festival commit should not widen to workitem metadata: %v", changed)
}

func TestIntegration_WorkitemCommit_MultiFestivalStagesResolvedScope(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-multi-festival"
	initWorkitemCommitCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	aaDir := dir + "/festivals/active/AA0001"
	zzDir := dir + "/festivals/active/ZZ0002"
	require.NoError(t, tc.WriteFile(aaDir+"/FESTIVAL_GOAL.md", "# AA0001\n"))
	require.NoError(t, tc.WriteFile(zzDir+"/FESTIVAL_GOAL.md", "# ZZ0002\n"))
	_, err := tc.RunCampInDir(dir, "workitem", "link", "timeline", "--festival", "AA0001")
	require.NoError(t, err)
	_, err = tc.RunCampInDir(dir, "workitem", "link", "timeline", "--festival", "ZZ0002")
	require.NoError(t, err)

	dryRun, err := tc.RunCampInDir(zzDir,
		"workitem", "commit", "--dry-run", "--json", "-m", "festival plan")
	require.NoError(t, err, "festival dry-run: %s", dryRun)
	var plan struct {
		FestivalRef string   `json:"festival_ref"`
		Tag         string   `json:"tag"`
		Stage       []string `json:"stage"`
	}
	require.NoError(t, json.Unmarshal([]byte(dryRun), &plan), "raw=%s", dryRun)

	assert.Equal(t, "ZZ0002", plan.FestivalRef)
	assert.Contains(t, plan.Tag, "FE-ZZ0002")
	assert.Contains(t, plan.Tag, ref)
	assert.Contains(t, plan.Stage, "festivals/active/ZZ0002/FESTIVAL_GOAL.md",
		"committing from ZZ0002 must stage ZZ0002 files: %v", plan.Stage)
	assert.NotContains(t, plan.Stage, "festivals/active/AA0001/FESTIVAL_GOAL.md",
		"committing from ZZ0002 must not stage the other festival's files: %v", plan.Stage)
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
	require.NoError(t, tc.WriteFile(dir+"/.campaign/workitems/links.yaml", "version: workitem-links/v1alpha1\nlinks: []\n"))

	out, err := tc.RunCampInDir(dir, "workitem", "commit", "timeline", "-m", "test widen")
	require.NoError(t, err, "camp workitem commit: %s", out)
	assert.Contains(t, out, ".campaign/workitems/links.yaml (link registry auto-included)",
		"plan output should explain why the registry is included:\n%s", out)

	changed := headChangedPaths(t, tc, dir)
	for _, p := range changed {
		if p == ".campaign/workitems/links.yaml" {
			continue
		}
		assert.True(t, strings.HasPrefix(p, "workflow/design/timeline/"),
			"unexpected path %q leaked past the workitem boundary; full set: %v", p, changed)
	}
	assert.Contains(t, changed, "workflow/design/timeline/test slug with spaces.md")
	assert.Contains(t, changed, ".campaign/workitems/links.yaml")
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

	jsonOut, err := tc.RunCampInDir(dir, "workitem", "commits", "timeline", "--json")
	require.NoError(t, err, "workitem commits --json: %s", jsonOut)
	assert.NotContains(t, jsonOut, "CampaignID")
	assert.NotContains(t, jsonOut, "WorkitemRef")

	var payload struct {
		SchemaVersion string `json:"schema_version"`
		Commits       []struct {
			Subject string `json:"subject"`
			Repo    string `json:"repo"`
			Tag     struct {
				CampaignID  string `json:"campaign_id"`
				QuestID     string `json:"quest_id"`
				FestRef     string `json:"fest_ref"`
				WorkitemRef string `json:"workitem_ref"`
			} `json:"tag"`
		} `json:"commits"`
	}
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &payload), "raw=%s", jsonOut)
	assert.Equal(t, "workitem-commits/v1alpha1", payload.SchemaVersion)
	require.NotEmpty(t, payload.Commits)

	subjects := make(map[string]bool)
	repos := make(map[string]bool)
	for _, commit := range payload.Commits {
		subjects[commit.Subject] = true
		repos[commit.Repo] = true
		assert.NotEmpty(t, commit.Tag.CampaignID)
		assert.Equal(t, "", commit.Tag.QuestID)
		assert.Equal(t, "", commit.Tag.FestRef)
		assert.Equal(t, ref, commit.Tag.WorkitemRef)
	}
	for _, expect := range []string{"root contribution", "project camp-A contribution", "project camp-B contribution"} {
		require.True(t, hasSubjectSuffix(subjects, expect), "missing commit subject suffix %q in %#v", expect, subjects)
	}
	for _, repoName := range []string{"camp-A", "camp-B"} {
		assert.True(t, repos["projects/"+repoName], "JSON missing repo field for %s: %#v", repoName, repos)
	}
}

func TestIntegration_WorkitemCommit_JSONContract(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-json"
	initWorkitemCommitCampaign(t, tc, dir)
	_ = seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/timeline/spec.md", "spec\n"))
	out, err := tc.RunCampInDir(dir,
		"workitem", "commit", "timeline",
		"--dry-run",
		"--json",
		"--include", "../outside",
	)
	require.NoError(t, err, "camp workitem commit --dry-run --json: %s", out)
	assert.NotContains(t, out, "\"Path\"")
	assert.NotContains(t, out, "\"Reason\"")

	var payload struct {
		SchemaVersion string   `json:"schema_version"`
		Stage         []string `json:"stage"`
		Skip          []struct {
			Path   string `json:"path"`
			Reason string `json:"reason"`
		} `json:"skip"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &payload), "raw=%s", out)
	assert.Equal(t, "workitem-commit/v1alpha1", payload.SchemaVersion)
	assert.NotEmpty(t, payload.Stage)
	require.Len(t, payload.Skip, 1)
	assert.Equal(t, "../outside", payload.Skip[0].Path)
	assert.Equal(t, "(out of scope)", payload.Skip[0].Reason)
}

func TestIntegration_WorkitemCommits_EmptyArray(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commits-empty-json"
	initWorkitemCommitCampaign(t, tc, dir)
	_ = seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	out, err := tc.RunCampInDir(dir, "workitem", "commits", "timeline", "--json")
	require.NoError(t, err, "workitem commits --json: %s", out)

	var payload struct {
		SchemaVersion string            `json:"schema_version"`
		Commits       []json.RawMessage `json:"commits"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &payload), "raw=%s", out)
	assert.Equal(t, "workitem-commits/v1alpha1", payload.SchemaVersion)
	assert.NotNil(t, payload.Commits)
	assert.Empty(t, payload.Commits)
}

func TestIntegration_WorkitemCommit_RefusesDirtyIndex(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-dirty"
	initWorkitemCommitCampaign(t, tc, dir)
	_ = seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	require.NoError(t, tc.WriteFile(dir+"/junk.txt", "leftover from earlier\n"))
	_, _, err := tc.ExecCommand("git", "-C", dir, "add", "junk.txt")
	require.NoError(t, err)

	indexBefore, _, ierr := tc.ExecCommand("git", "-C", dir, "diff", "--cached", "--name-only")
	require.NoError(t, ierr)

	out, _, err := tc.ExecCommand("sh", "-c",
		"cd "+dir+" && /camp workitem commit timeline -m 'design: should refuse' 2>&1; echo EXIT=$?")
	require.NoError(t, err)
	assert.Contains(t, out, "git reset HEAD",
		"refusal must point user at git reset HEAD (NOT --hard): %s", out)
	assert.NotContains(t, out, "EXIT=0",
		"commit must NOT succeed when index is dirty: %s", out)

	indexAfter, _, ierr := tc.ExecCommand("git", "-C", dir, "diff", "--cached", "--name-only")
	require.NoError(t, ierr)
	assert.Equal(t, strings.TrimSpace(indexBefore), strings.TrimSpace(indexAfter),
		"index must be unchanged after the refusal (no staging happened)")

	stagedOut, err := tc.RunCampInDir(dir, "workitem", "commit", "timeline",
		"-m", "design: explicit staged", "--staged")
	require.NoError(t, err, "--staged escape hatch must succeed: %s", stagedOut)
}

func TestIntegration_WorkitemCommit_StagedLinkedProject(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-staged-linked"
	initWorkitemCommitCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	projDir := dir + "/projects/camp-timeline"
	require.NoError(t, tc.CreateGitRepo(projDir))
	_, err := tc.RunCampInDir(dir, "workitem", "link", "timeline", "--project", "camp-timeline")
	require.NoError(t, err)

	rootHead, _, herr := tc.ExecCommand("git", "-C", dir, "rev-parse", "HEAD")
	require.NoError(t, herr)
	rootHead = strings.TrimSpace(rootHead)

	require.NoError(t, tc.WriteFile(projDir+"/foo.go", "package x\n"))
	_, _, err = tc.ExecCommand("git", "-C", projDir, "add", "foo.go")
	require.NoError(t, err)

	out, err := tc.RunCampInDir(projDir, "workitem", "commit",
		"--workitem", "timeline", "--staged", "-m", "feat: staged")
	require.NoError(t, err, "workitem commit --staged: %s", out)

	projHead, _, herr := tc.ExecCommand("git", "-C", projDir, "log", "-1", "--pretty=%H %s")
	require.NoError(t, herr)
	assert.Contains(t, projHead, ref,
		"submodule HEAD must include WI-<ref>: %s", projHead)

	rootHeadAfter, _, herr := tc.ExecCommand("git", "-C", dir, "rev-parse", "HEAD")
	require.NoError(t, herr)
	assert.Equal(t, rootHead, strings.TrimSpace(rootHeadAfter),
		"campaign root HEAD must not advance when --staged was routed through the submodule")
}

func TestIntegration_WorkitemCommit_JSONSchemaVersion(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commit-json-schema"
	initWorkitemCommitCampaign(t, tc, dir)
	_ = seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	require.NoError(t, tc.WriteFile(dir+"/workflow/design/timeline/notes.md", "notes\n"))

	out, err := tc.RunCampInDir(dir, "workitem", "commit", "timeline",
		"-m", "design: timeline notes", "--json")
	require.NoError(t, err, "workitem commit --json: %s", out)
	jsonStart := strings.Index(out, "{")
	require.GreaterOrEqual(t, jsonStart, 0, "no JSON object in output: %s", out)
	payload := out[jsonStart:]

	var got struct {
		SchemaVersion string `json:"schema_version"`
	}
	require.NoError(t, json.Unmarshal([]byte(payload), &got), "parse: %s", payload)
	assert.Equal(t, "workitem-commit/v1alpha1", got.SchemaVersion,
		"workitem commit --json must declare schema_version=workitem-commit/v1alpha1, got: %s", payload)
}

func TestIntegration_WorkitemCommits_JSONSchemaVersion(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/wi-commits-json-schema"
	initWorkitemCommitCampaign(t, tc, dir)
	_ = seedDesignWorkitemWithRef(t, tc, dir, "timeline")

	out, err := tc.RunCampInDir(dir, "workitem", "commits", "timeline", "--json")
	require.NoError(t, err, "workitem commits --json: %s", out)
	jsonStart := strings.Index(out, "{")
	require.GreaterOrEqual(t, jsonStart, 0, "no JSON object in output: %s", out)
	payload := out[jsonStart:]

	var got struct {
		SchemaVersion string `json:"schema_version"`
	}
	require.NoError(t, json.Unmarshal([]byte(payload), &got), "parse: %s", payload)
	assert.Equal(t, "workitem-commits/v1alpha1", got.SchemaVersion,
		"workitem commits --json must declare schema_version=workitem-commits/v1alpha1, got: %s", payload)
}

func TestIntegration_WorkitemCommit_SymlinkedCampaignRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	real := "/test/wi-commit-sym-real"
	link := "/test/wi-commit-sym-link"
	initWorkitemCommitCampaign(t, tc, real)
	ref := seedDesignWorkitemWithRef(t, tc, real, "timeline")

	require.NoError(t, tc.CreateGitRepo(real+"/projects/camp-timeline"))
	_, err := tc.RunCampInDir(real, "workitem", "link", "timeline", "--project", "camp-timeline")
	require.NoError(t, err)

	_, code, err := tc.ExecCommand("ln", "-s", real, link)
	require.NoError(t, err)
	require.Equal(t, 0, code)

	rootHead, _, herr := tc.ExecCommand("git", "-C", real, "rev-parse", "HEAD")
	require.NoError(t, herr)
	rootHead = strings.TrimSpace(rootHead)

	require.NoError(t, tc.WriteFile(real+"/projects/camp-timeline/foo.go", "package x\n"))
	out, err := tc.RunCampInDir(link+"/projects/camp-timeline",
		"workitem", "commit", "--workitem", "timeline", "-m", "feat: stub via symlink")
	require.NoError(t, err, "camp workitem commit via symlink: %s", out)

	subject := lastCommitSubject(t, tc, real+"/projects/camp-timeline")
	assert.Contains(t, subject, ref,
		"project subject must include WI-<ref> after symlinked-cwd commit: %s", subject)

	rootHeadAfter, _, herr := tc.ExecCommand("git", "-C", real, "rev-parse", "HEAD")
	require.NoError(t, herr)
	assert.Equal(t, rootHead, strings.TrimSpace(rootHeadAfter),
		"campaign root HEAD must not advance when commit was routed via symlinked cwd")
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

	stdoutPath := "/tmp/workitem-commit-json-error-stdout"
	stderrPath := "/tmp/workitem-commit-json-error-stderr"
	_, code, err = tc.ExecCommand("sh", "-c",
		"cd "+dir+" && /camp workitem commit --json -m 'no context' >"+stdoutPath+" 2>"+stderrPath)
	require.NoError(t, err)
	require.Equal(t, 2, code)

	stdout, err := tc.ReadFile(stdoutPath)
	require.NoError(t, err)
	assert.Empty(t, stdout)
	stderr, err := tc.ReadFile(stderrPath)
	require.NoError(t, err)
	assert.NotContains(t, stderr, "Usage:")

	var envelope struct {
		SchemaVersion string `json:"schema_version"`
		Error         struct {
			Code     string `json:"code"`
			Message  string `json:"message"`
			Hint     string `json:"hint"`
			ExitCode int    `json:"exit_code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(stderr), &envelope), "stderr=%s", stderr)
	assert.Equal(t, "workitem-commit/v1alpha1", envelope.SchemaVersion)
	assert.Equal(t, "validation_error", envelope.Error.Code)
	assert.Equal(t, "no workitem context resolved from cwd", envelope.Error.Message)
	assert.Contains(t, envelope.Error.Hint, "camp workitem current <selector>")
	assert.Equal(t, 2, envelope.Error.ExitCode)
}
