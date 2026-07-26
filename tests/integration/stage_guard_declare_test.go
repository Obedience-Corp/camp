//go:build integration
// +build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMixedRootCampaign builds the design's flagship fixture: a directory
// holding a tracked script, an over-threshold "video", and a new small note.
// The note is criterion 2, the data-loss regression: it must reach git.
func setupMixedRootCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()

	path := "/campaigns/" + name
	_, err := tc.InitCampaign(path, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		cat >> .campaign/campaign.yaml <<'YAML'
commit:
  guards:
    max_file_size: 1MiB
YAML
		mkdir -p videos/my-video
		printf 'shot list' > videos/my-video/script.md
		git add videos/my-video/script.md
		git -c user.email=t@t -c user.name=t commit -q -m "track the script"

		dd if=/dev/zero of=videos/my-video/footage.mp4 bs=1024 count=3072 2>/dev/null
		printf 'new note beside the footage' > videos/my-video/notes.md
	`, path))

	return path
}

// Criteria 1 and 2: the over-threshold file is excluded and a root declared,
// while the new small file beside it still reaches git.
func TestIntegration_GuardAutoDeclaresMixedRoot(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupMixedRootCampaign(t, tc, "guard-declare-mixed")

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "rough cut notes")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "videos/my-video/ is now an artifact root")
	assert.Contains(t, output, "footage.mp4")
	assert.Contains(t, output, "Undo: camp artifacts remove videos/my-video",
		"every message announcing new behavior must carry its undo")
	assert.Contains(t, output, "syncs with 'camp sync --from <machine>'")
	// Design doc 02's "script.md and notes.md stay with git" line. Without it
	// the user has been told their directory is now artifact content and has
	// no way to know the note they just wrote is still versioned.
	assert.Contains(t, output, "stay with git")
	assert.Contains(t, output, "notes.md")

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")

	// Criterion 2, the data-loss regression. A new small file inside a mixed
	// root must become git's, not silently reclassified as artifact content.
	assert.Contains(t, committed, "videos/my-video/notes.md",
		"a new small file in a MIXED root must land in git")
	assert.NotContains(t, committed, "videos/my-video/footage.mp4",
		"the over-threshold file must be excluded")

	// The commit carries its own bookkeeping.
	assert.Contains(t, committed, ".campaign/artifacts.yaml",
		"the declaration must land in the same commit that excluded the bytes")

	declared, err := tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
	require.NoError(t, err)
	assert.Contains(t, declared, "videos/my-video")

	// A mixed root is never gitignored: the rule would hide tracked content.
	gitignore := readGitignore(t, tc, campPath)
	assert.NotContains(t, gitignore, "videos/my-video/",
		"a mixed root must never get an ignore rule")
}

// Criterion 9: refusal is per file, not per commit. The commit proceeds and
// only the refused file is left out.
func TestIntegration_GuardRefusesCampaignRootFilePerFile(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-refuse-root")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		dd if=/dev/zero of=render.mov bs=1024 count=3072 2>/dev/null
		printf 'ordinary' > ordinary.md
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "root-level media")
	require.NoError(t, err, "the commit must still succeed; output:\n%s", output)

	assert.Contains(t, output, "render.mov")
	assert.Contains(t, output, "sits at the campaign root; camp will not make the campaign an artifact root")
	assert.Contains(t, output, "camp artifacts add")

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "ordinary.md", "everything else must still commit")
	assert.NotContains(t, committed, "render.mov")

	// Camp must not have declared anything for a refusal.
	exists, err := tc.CheckFileExists(campPath + "/.campaign/artifacts.yaml")
	require.NoError(t, err)
	if exists {
		declared, err := tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
		require.NoError(t, err)
		assert.NotContains(t, declared, "render.mov")
	}
}

// Camp must never turn its own state tree into a user artifact root.
func TestIntegration_GuardRefusesCampaignStateTree(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-refuse-state")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p .campaign/graphs
		dd if=/dev/zero of=.campaign/graphs/graph.png bs=1024 count=3072 2>/dev/null
		printf 'ordinary' > ordinary.md
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "state tree export")
	require.NoError(t, err, "output:\n%s", output)

	if exists, _ := tc.CheckFileExists(campPath + "/.campaign/artifacts.yaml"); exists {
		declared, err := tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
		require.NoError(t, err)
		assert.NotContains(t, declared, ".campaign/graphs",
			"camp's own state tree must never become a user artifact root")
	}

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "ordinary.md")
}

// A clean root does get its ignore rule, which makes everything landing there
// later artifact content by construction.
func TestIntegration_GuardAutoDeclaresCleanRootWithIgnoreRule(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-declare-clean")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media/renders
		dd if=/dev/zero of=media/renders/frame.exr bs=1024 count=3072 2>/dev/null
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "renders")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "media/renders/ is now an artifact root")
	assert.Contains(t, output, "camp sync")

	gitignore := readGitignore(t, tc, campPath)
	assert.Equal(t, 1, countRuleLines(gitignore, "media/renders/"),
		"a clean root gets exactly one ignore rule; got:\n%s", gitignore)
}

// Criterion 10: a second over-threshold file inside an already-declared root
// reuses it and reports one tally line rather than the full teaching block.
func TestIntegration_GuardReusesExistingRootWithTallyLine(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupMixedRootCampaign(t, tc, "guard-reuse-root")

	_, err := tc.RunCampInDir(campPath, "commit", "-m", "first cut")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		dd if=/dev/zero of=videos/my-video/broll.mp4 bs=1024 count=3072 2>/dev/null
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "b-roll")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "kept out of git",
		"an existing root should report a tally line")
	// The b-roll was the only change, so nothing remains staged. Camp must
	// report that as a clean no-op rather than letting git reject an empty
	// commit: the excluded file is still an unstaged worktree change, so the
	// ordinary has-changes check would otherwise say yes.
	assert.Contains(t, output, "Nothing left to commit")
	assert.NotContains(t, output, "is now an artifact root",
		"an already-declared root must not re-teach what an artifact root is")

	declared, err := tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
	require.NoError(t, err)
	assert.Equal(t, 1, countRuleLines(declared, "- path: videos/my-video"),
		"the root must not be declared twice; got:\n%s", declared)
}

// Criteria 8 and 8b: inside a project camp excludes, asks, and declares nothing.
func TestIntegration_GuardProjectExcludesAndAsks(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, projPath, _ := setupFreshCampaignWithSubmodule(t, tc, "guard-project-ask")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		cat >> .campaign/campaign.yaml <<'YAML'
commit:
  guards:
    max_project_file_size: 1MiB
YAML
	`, campPath))

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p testdata
		dd if=/dev/zero of=testdata/corpus.tar bs=1024 count=3072 2>/dev/null
		printf 'fixture note' > testdata/README.md
	`, projPath))

	output, err := tc.RunCampInDir(projPath, "project", "commit", "-m", "add fixture")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "testdata/corpus.tar")
	assert.Contains(t, output, "was left out of this commit")
	assert.Contains(t, output, "if it is build output")
	assert.Contains(t, output, "git lfs track")
	assert.Contains(t, output, "commit.guards.allow")

	// Camp must never declare an artifact root for project content.
	if exists, _ := tc.CheckFileExists(campPath + "/.campaign/artifacts.yaml"); exists {
		declared, err := tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
		require.NoError(t, err)
		assert.NotContains(t, declared, "testdata",
			"an artifact root inside a project would keep bytes off its remote")
	}
}

// Criteria 7 and 7b: a tracked file over the threshold is committed anyway and
// reported every time, with the untrack remedy and the allowlist line.
func TestIntegration_GuardReportsTrackedGrowthButCommitsIt(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "guard-tracked-growth")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p graphs
		printf 'small' > graphs/graph.png
		git add graphs/graph.png
		git -c user.email=t@t -c user.name=t commit -q -m "track the graph"

		dd if=/dev/zero of=graphs/graph.png bs=1024 count=3072 2>/dev/null
	`, campPath))

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "regenerated graph")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "graphs/graph.png is tracked and now")
	assert.Contains(t, output, "Tracked files are always committed")
	assert.Contains(t, output, "git rm --cached graphs/graph.png")
	assert.Contains(t, output, "commit.guards.allow")

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "graphs/graph.png",
		"tracked growth is reported, never excluded")
}
