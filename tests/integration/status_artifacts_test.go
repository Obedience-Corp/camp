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

// setupCommittedMixedRoot runs the real auto-declaring commit on the mixed-root
// fixture, then leaves the working tree in the state the status fix is about:
// footage camp already keeps out of git, plus files that are still git's.
func setupCommittedMixedRoot(t *testing.T, tc *TestContainer, name string) string {
	t.Helper()
	campPath := setupMixedRootCampaign(t, tc, name)

	output, err := tc.RunCampInDir(campPath, "commit", "-m", "rough cut notes")
	require.NoError(t, err, "output:\n%s", output)
	require.Contains(t, output, "videos/my-video/ is now an artifact root")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p videos/my-video/takes renders
		dd if=/dev/zero of=videos/my-video/takes/take1.mp4 bs=1024 count=2048 2>/dev/null
		printf 'todo beside the footage' > videos/my-video/todo.md
		dd if=/dev/zero of=renders/final.mov bs=1024 count=2048 2>/dev/null
	`, campPath))
	return campPath
}

// splitArtifactSection separates git's own output from camp's section.
func splitArtifactSection(t *testing.T, stdout string) (gitPart, artifactPart string) {
	t.Helper()
	gitPart, artifactPart, found := strings.Cut(stdout, "2 artifacts ·")
	require.True(t, found, "camp status must report artifact content; stdout:\n%s", stdout)
	return gitPart, artifactPart
}

func TestIntegration_StatusMovesMixedRootArtifactsOutOfUntracked(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupCommittedMixedRoot(t, tc, "status-artifacts-long")

	raw := tc.GitOutput(t, campPath, "status", "--porcelain")
	require.Contains(t, raw, "videos/my-video/footage.mp4",
		"git alone still calls the footage untracked; that is the confusion being fixed")

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "status")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	gitPart, artifactPart := splitArtifactSection(t, stdout)

	assert.NotContains(t, gitPart, "footage.mp4", "artifact content must leave git's untracked list")
	assert.NotContains(t, gitPart, "takes/", "a directory holding only artifact content must leave it too")
	assert.Contains(t, gitPart, "videos/my-video/todo.md",
		"a small new file in a mixed root is git's; size decides, not membership")
	assert.Contains(t, gitPart, "renders/",
		"an over-threshold file outside any declared root is still undecided and stays untracked")

	assert.Contains(t, artifactPart, "5.0 MB kept out of git")
	assert.Contains(t, artifactPart, "camp artifacts")
	assert.NotContains(t, artifactPart, "footage.mp4")
	assert.NotContains(t, artifactPart, "renders/final.mov")
	assert.NotContains(t, artifactPart, "todo.md")
}

func TestIntegration_StatusShortAndPorcelainReportArtifacts(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupCommittedMixedRoot(t, tc, "status-artifacts-short")
	summary := "2 artifacts · 5.0 MB kept out of git → camp artifacts"

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "status", "-s")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)
	assert.Contains(t, stdout, "?? videos/my-video/todo.md")
	assert.NotContains(t, stdout, "?? videos/my-video/footage.mp4")
	assert.NotContains(t, stdout, "?? videos/my-video/takes/")
	assert.Contains(t, stdout, summary)

	stdout, stderr, exitCode, err = tc.RunCampSplitInDir(campPath, "status", "--", "--porcelain")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)
	assert.Contains(t, stdout, "?? videos/my-video/todo.md")
	assert.NotContains(t, stdout, "footage.mp4")
	assert.NotContains(t, stdout, "artifact content",
		"porcelain stdout is parsed by scripts; camp's line must not land there")
	assert.Contains(t, stderr, summary)

	stdout, _, exitCode, err = tc.RunCampSplitInDir(campPath, "status", "--", "--porcelain", "videos/my-video/takes")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode)
	assert.NotContains(t, stdout, "take1.mp4", "exclusions must compose with a user pathspec")
}

// Under block the commit refuses the footage, under off it commits it, and an
// allowlisted file is always committed: in each case git's "untracked" is the
// truth and camp status must not reclassify it.
func TestIntegration_StatusLeavesNonExcludedFilesUntracked(t *testing.T) {
	cases := []struct {
		name  string
		guard string
	}{
		{"block", "    large_files: block"},
		{"off", "    large_files: off"},
		{"allow", "    allow:\n      - \"*.mp4\""},
	}
	tc := GetSharedContainer(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			campPath := setupMixedRootCampaign(t, tc, "status-artifacts-"+c.name)
			tc.Shell(t, fmt.Sprintf(`
				cd %s
				printf '%%s\n' '%s' >> .campaign/campaign.yaml
				printf 'version: 1\nroots:\n    - path: videos/my-video\n' > .campaign/artifacts.yaml
			`, campPath, strings.ReplaceAll(c.guard, "\n", "' '")))

			stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "status", "-s")
			require.NoError(t, err)
			require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)
			assert.Contains(t, stdout, "?? videos/my-video/footage.mp4")
			assert.NotContains(t, stdout, "artifact content")
			assert.NotContains(t, stderr, "artifact content not checked")
		})
	}
}

func TestIntegration_StatusWithoutArtifactRootsIsGitStatus(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupGuardCampaign(t, tc, "status-artifacts-none")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p media
		dd if=/dev/zero of=media/clip.mp4 bs=1024 count=3072 2>/dev/null
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "status", "-s")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)
	assert.Contains(t, stdout, "?? media/")
	assert.NotContains(t, stdout, "artifact content")
}

func TestIntegration_StatusAllCountsArtifactsSeparately(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupSubmoduleCampaign(t, tc, "status-artifacts-all")
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'commit:\n  guards:\n    max_file_size: 1MiB\n' >> .campaign/campaign.yaml
		mkdir -p videos/my-video
		printf 'shot list' > videos/my-video/script.md
		printf 'version: 1\nroots:\n    - path: videos/my-video\n' > .campaign/artifacts.yaml
		git add .campaign videos/my-video/script.md
		git -c user.email=t@t -c user.name=t commit -q -m "declare the video root"
		dd if=/dev/zero of=videos/my-video/footage.mp4 bs=1024 count=3072 2>/dev/null
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "status", "all", "--json")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	var doc struct {
		Repos []struct {
			Name      string `json:"name"`
			Clean     bool   `json:"clean"`
			Untracked int    `json:"untracked"`
			Artifacts int    `json:"artifacts"`
		} `json:"repos"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "stdout:\n%s", stdout)
	require.NotEmpty(t, doc.Repos)
	root := doc.Repos[0]
	assert.Equal(t, "camp root", root.Name)
	assert.Equal(t, 0, root.Untracked)
	assert.Equal(t, 1, root.Artifacts)
	assert.True(t, root.Clean, "artifact content alone must not make the camp root dirty")
}

func TestIntegration_StatusArtifactsRespectPathspecs(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupCommittedMixedRoot(t, tc, "status-artifacts-scoped")
	tc.Shell(t, fmt.Sprintf("mkdir -p %s/docs; printf note > %s/docs/new.md", campPath, campPath))
	cases := []struct {
		name         string
		args         []string
		want, absent string
	}{
		{"unrelated directory", []string{"--", "docs"}, "", "kept out of git"},
		{"one artifact directory", []string{"--", "videos/my-video/takes"}, "1 artifact · 2.0 MB", "videos/my-video/footage.mp4"},
		{"glob", []string{"--", ":(glob)videos/**/*.mp4"}, "2 artifacts · 5.0 MB", "todo.md"},
		{"exclude", []string{"--", "videos", ":(exclude)videos/my-video/takes"}, "1 artifact · 3.0 MB", "take1.mp4"},
		{"explicit separator", []string{"--", "--", "docs"}, "docs/new.md", "kept out of git"},
		{"short explicit separator", []string{"-s", "--", "--", "docs"}, "?? docs/new.md", "artifact content"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"status"}, tt.args...)
			stdout, stderr, code, err := tc.RunCampSplitInDir(campPath, args...)
			require.NoError(t, err)
			require.Zero(t, code, "stderr: %s", stderr)
			if tt.want != "" {
				assert.Contains(t, stdout, tt.want)
			}
			assert.NotContains(t, stdout, tt.absent)
		})
	}
	stdout, stderr, code, err := tc.RunCampSplitInDir(campPath, "status", "--", "--porcelain=v2", "-z", "videos/my-video/takes")
	require.NoError(t, err)
	require.Zero(t, code, "stderr: %s", stderr)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "1 artifact · 2.0 MB")
}

func TestIntegration_StatusLargeArtifactCollection(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupSubmoduleCampaign(t, tc, "status-artifacts-large")
	writeGuardConfig(t, tc, campPath, "    max_file_size: 1MiB")
	tc.Shell(t, fmt.Sprintf(`
  cd %s
  mkdir -p videos
  printf 'tracked notes' > videos/notes.md
  printf 'version: 1\nroots:\n    - path: videos\n' > .campaign/artifacts.yaml
  git add .
  git -c user.email=t@t -c user.name=t commit -qm fixture
  prefix=$(printf '%%0180d' 0)
  i=0
  while [ "$i" -lt 1200 ]; do
   truncate -s 2097152 "videos/$prefix-$i.mp4"
   i=$((i+1))
  done
 `, campPath))
	// A constrained stack reproduces the OS argument limit with a small fixture.
	output, code, err := tc.ExecCommand("sh", "-c", fmt.Sprintf("ulimit -s 256; cd %s; %s/camp status -s", campPath, tc.campEnvPrefix()))
	require.NoError(t, err)
	require.Zero(t, code, "output: %s", output)
	assert.Contains(t, output, "showing plain git status including artifact content")
	assert.NotContains(t, output, "argument list too long")

	output, code, err = tc.ExecCommand("sh", "-c", fmt.Sprintf("ulimit -s 256; cd %s; %s/camp status all --json", campPath, tc.campEnvPrefix()))
	require.NoError(t, err)
	require.Zero(t, code, "output: %s", output)
	// ExecCommand combines streams; suppress notices for machine readback.
	stdout, stderr, code, err := tc.RunCampSplitInDir(campPath, "status", "all", "--json")
	require.NoError(t, err)
	require.Zero(t, code, "stderr: %s", stderr)
	var doc struct {
		Repos []struct {
			Clean                bool
			Untracked, Artifacts int
			Error                string
		}
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	require.NotEmpty(t, doc.Repos)
	assert.Empty(t, doc.Repos[0].Error)
	assert.Equal(t, 1200, doc.Repos[0].Artifacts)
	assert.Zero(t, doc.Repos[0].Untracked)
	assert.True(t, doc.Repos[0].Clean)
}
