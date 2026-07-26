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

// setupArtifactsCampaign builds a campaign with one clean media root and one
// mixed root. Fixtures are constructed in-container: no test may point at the
// real campaign or at any host media directory.
func setupArtifactsCampaign(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()

	path := "/campaigns/" + name
	_, err := tc.InitCampaign(path, name, "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media/renders
		printf 'render-bytes' > media/renders/frame001.exr
		printf 'render-bytes' > media/renders/frame002.exr

		mkdir -p videos
		printf 'script text' > videos/script.md
		printf 'notes text' > videos/notes.md
		printf 'footage-bytes' > videos/footage.mov
		printf 'footage-bytes' > videos/b-roll.mov

		mkdir -p empty-root

		git add videos/script.md videos/notes.md
		git -c user.email=t@t -c user.name=t commit -q -m "track video scripts"
	`, path))

	return path
}

// readGitignore returns the campaign root .gitignore, or "" when absent.
func readGitignore(t *testing.T, tc *TestContainer, campPath string) string {
	t.Helper()
	exists, err := tc.CheckFileExists(campPath + "/.gitignore")
	require.NoError(t, err)
	if !exists {
		return ""
	}
	content, err := tc.ReadFile(campPath + "/.gitignore")
	require.NoError(t, err)
	return content
}

// countRuleLines counts active (non-comment) lines exactly equal to rule.
func countRuleLines(content, rule string) int {
	n := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == rule {
			n++
		}
	}
	return n
}

// Criterion 22: a clean root gets its ignore rule written, and re-declaring it
// does not append a second copy.
func TestIntegration_ArtifactsAddCleanRootWritesGitignore(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupArtifactsCampaign(t, tc, "artifacts-clean")

	output, err := tc.RunCampInDir(campPath, "artifacts", "add", "media/renders")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "Declared artifact root media/renders")
	assert.Contains(t, output, "Added 'media/renders/' to .gitignore")
	assert.Contains(t, output, "lives outside git and syncs between your",
		"clean-root output must state the consequence, not just the edit")
	assert.Contains(t, output, "Undo: camp artifacts remove media/renders")

	gitignore := readGitignore(t, tc, campPath)
	require.Equal(t, 1, countRuleLines(gitignore, "media/renders/"),
		"rule should be present exactly once; got:\n%s", gitignore)

	// git must actually agree the path is ignored now.
	ignored := tc.Shell(t, fmt.Sprintf(
		`cd %s && git check-ignore -q media/renders && echo IGNORED || echo VISIBLE`, campPath))
	assert.Contains(t, ignored, "IGNORED")

	// Re-declaring is append-if-missing, not append-again.
	_, _ = tc.RunCampInDir(campPath, "artifacts", "add", "media/renders")
	gitignore = readGitignore(t, tc, campPath)
	assert.Equal(t, 1, countRuleLines(gitignore, "media/renders/"),
		"re-running must not duplicate the rule; got:\n%s", gitignore)
}

// Criterion 22: --no-gitignore declares without writing and keeps the warning.
func TestIntegration_ArtifactsAddNoGitignoreSkipsWrite(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupArtifactsCampaign(t, tc, "artifacts-nogi")

	before := readGitignore(t, tc, campPath)

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath,
		"artifacts", "add", "media/renders", "--no-gitignore")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	assert.Contains(t, stdout, "Declared artifact root media/renders")
	assert.NotContains(t, stdout, "Added 'media/renders/' to .gitignore")
	assert.Contains(t, stderr, "is not gitignored",
		"--no-gitignore must retain the existing warning")

	assert.Equal(t, before, readGitignore(t, tc, campPath),
		".gitignore must be byte-identical after --no-gitignore")
}

// Criterion 23: a mixed root is declared, never gitignored, and its split is
// reported with real counts.
func TestIntegration_ArtifactsAddMixedRootReportsSplit(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupArtifactsCampaign(t, tc, "artifacts-mixed")

	before := readGitignore(t, tc, campPath)

	output, err := tc.RunCampInDir(campPath, "artifacts", "add", "videos")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "Declared artifact root videos (mixed)")
	assert.Contains(t, output, "2 git-tracked files stay with git and are excluded from artifact sync")
	assert.Contains(t, output, "2 untracked files")
	assert.Contains(t, output, "are the artifact set")
	assert.Contains(t, output, ".gitignore not modified: ignoring this root would hide tracked content")

	assert.Equal(t, before, readGitignore(t, tc, campPath),
		"a mixed root must never touch .gitignore")

	declared, err := tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
	require.NoError(t, err)
	assert.Contains(t, declared, "videos", "the declaration must still be saved")
}

// Criterion 24: --dry-run reports and writes nothing at all.
func TestIntegration_ArtifactsAddDryRunWritesNothing(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupArtifactsCampaign(t, tc, "artifacts-dryrun")

	gitignoreBefore := readGitignore(t, tc, campPath)
	yamlExistsBefore, err := tc.CheckFileExists(campPath + "/.campaign/artifacts.yaml")
	require.NoError(t, err)
	var yamlBefore string
	if yamlExistsBefore {
		yamlBefore, err = tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
		require.NoError(t, err)
	}

	output, err := tc.RunCampInDir(campPath, "artifacts", "add", "videos", "--dry-run")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "videos would be declared as an artifact root (policy: always)")
	assert.Contains(t, output, "4 files")
	assert.Contains(t, output, "tracked")
	assert.Contains(t, output, "untracked")
	assert.Contains(t, output, "ignored")
	assert.Contains(t, output, "Nothing was written")
	assert.NotContains(t, output, "Declared artifact root")

	assert.Equal(t, gitignoreBefore, readGitignore(t, tc, campPath),
		"--dry-run must leave .gitignore byte-identical")

	yamlExistsAfter, err := tc.CheckFileExists(campPath + "/.campaign/artifacts.yaml")
	require.NoError(t, err)
	assert.Equal(t, yamlExistsBefore, yamlExistsAfter,
		"--dry-run must not create artifacts.yaml")
	if yamlExistsBefore && yamlExistsAfter {
		yamlAfter, err := tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
		require.NoError(t, err)
		assert.Equal(t, yamlBefore, yamlAfter,
			"--dry-run must leave artifacts.yaml byte-identical")
	}
}

// An empty directory has no tracked files, so it is clean and gets its rule.
func TestIntegration_ArtifactsAddEmptyRootIsClean(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupArtifactsCampaign(t, tc, "artifacts-empty")

	output, err := tc.RunCampInDir(campPath, "artifacts", "add", "empty-root")
	require.NoError(t, err, "output:\n%s", output)

	assert.Contains(t, output, "Declared artifact root empty-root")
	assert.Contains(t, output, "Added 'empty-root/' to .gitignore")
	assert.NotContains(t, output, "(mixed)")

	gitignore := readGitignore(t, tc, campPath)
	assert.Equal(t, 1, countRuleLines(gitignore, "empty-root/"))
}

// --dry-run never reaches File.Add, so path and policy validation has to run
// before the survey or an impossible declaration would be reported as viable.
func TestIntegration_ArtifactsAddDryRunStillValidates(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupArtifactsCampaign(t, tc, "artifacts-dryrun-invalid")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "escaping path",
			args: []string{"artifacts", "add", "../outside", "--dry-run"},
			want: "escapes the campaign root",
		},
		{
			name: "path under .campaign",
			args: []string{"artifacts", "add", ".campaign/cache", "--dry-run"},
			want: "may not live under .campaign",
		},
		{
			name: "unknown policy",
			args: []string{"artifacts", "add", "media/renders", "--policy", "sometimes", "--dry-run"},
			want: "unknown policy",
		},
	}

	for _, tc2 := range cases {
		t.Run(tc2.name, func(t *testing.T) {
			stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, tc2.args...)
			require.NoError(t, err)
			assert.NotEqual(t, 0, exitCode, "stdout:\n%s", stdout)
			assert.Contains(t, stdout+stderr, tc2.want)
			assert.NotContains(t, stdout, "would be declared",
				"an invalid declaration must not be reported as viable")
		})
	}
}
