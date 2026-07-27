//go:build integration
// +build integration

package integration

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupBigFilesCampaign builds one fixture covering every ownership state the
// sweep must distinguish.
func setupBigFilesCampaign(t *testing.T, tc *TestContainer, name string) string {
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

		# Tracked and over threshold: already in history, past the guard.
		mkdir -p tracked
		dd if=/dev/zero of=tracked/blob.bin bs=1024 count=3072 2>/dev/null
		git add -f tracked/blob.bin
		git -c user.email=t@t -c user.name=t commit -q -m "track a large blob"

		# Gitignored and undeclared: owned by no system. The motivating case.
		mkdir -p media/raw
		dd if=/dev/zero of=media/raw/footage.mov bs=1024 count=3072 2>/dev/null
		printf 'media/raw/\n' >> .gitignore

		# Gitignored but inside a declared root: sync owns it, so it must NOT
		# be reported. Reporting it would tell the user to fix what they fixed.
		mkdir -p declared
		dd if=/dev/zero of=declared/asset.bin bs=1024 count=3072 2>/dev/null
		printf 'declared/\n' >> .gitignore
		printf 'version: 1\nroots:\n  - path: declared\n' > .campaign/artifacts.yaml

		# LFS-attributed: another system already owns it.
		mkdir -p lfsdir
		dd if=/dev/zero of=lfsdir/big.psd bs=1024 count=3072 2>/dev/null
		printf 'lfsdir/*.psd filter=lfs diff=lfs merge=lfs -text\n' > .gitattributes
		printf 'lfsdir/\n' >> .gitignore
	`, path))

	return path
}

// bigFilesFindings returns the sweep's findings keyed by path.
func bigFilesFindings(t *testing.T, tc *TestContainer, campPath string) map[string]string {
	t.Helper()
	stdout, _, _, err := tc.RunCampSplitInDir(campPath, "doctor", "-c", "bigfiles", "--json")
	require.NoError(t, err)

	var doc doctorJSONDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc),
		"doctor -c bigfiles --json must emit one document; got:\n%s", stdout)

	byPath := make(map[string]string)
	for _, issue := range doc.Issues {
		if issue.CheckID != "bigfiles" {
			continue
		}
		p, _ := issue.Details["path"].(string)
		state, _ := issue.Details["state"].(string)
		byPath[p] = state
	}
	return byPath
}

// Criteria 29 and 30: the sweep finds what the commit path cannot see, and
// leaves alone what another system already owns.
func TestIntegration_DoctorBigFilesOwnership(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupBigFilesCampaign(t, tc, "doctor-bigfiles")

	found := bigFilesFindings(t, tc, campPath)

	assert.Equal(t, "tracked_in_history", found["tracked/blob.bin"],
		"a large blob already committed must be reported; the guard stops the next one, not the last one")
	assert.Equal(t, "owned_by_no_system", found["media/raw/footage.mov"],
		"a gitignored, undeclared large file is the motivating case: it moves by no mechanism")

	assert.NotContains(t, found, "declared/asset.bin",
		"a file inside a declared artifact root is owned by sync and must not be reported")
	assert.NotContains(t, found, "lfsdir/big.psd",
		"an LFS-managed file is owned by LFS and must not be reported")
}

// Files under the threshold must not appear, whatever their extension. This is
// the analog of criterion 30's real-campaign result, where the node_modules
// files sit below the limit and must stay out of the report.
func TestIntegration_DoctorBigFilesIgnoresUnderThreshold(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupBigFilesCampaign(t, tc, "doctor-bigfiles-small")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		mkdir -p node_modules/pkg
		for i in $(seq 1 20); do dd if=/dev/zero of=node_modules/pkg/m$i.js bs=1024 count=64 2>/dev/null; done
		printf 'node_modules/\n' >> .gitignore
	`, campPath))

	found := bigFilesFindings(t, tc, campPath)
	for path := range found {
		assert.NotContains(t, path, "node_modules",
			"files under the threshold must not appear regardless of directory name")
	}
}

// The sweep uses the guard's thresholds, so doctor and commit never disagree
// about what "large" means.
func TestIntegration_DoctorBigFilesHonorsConfiguredThreshold(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupBigFilesCampaign(t, tc, "doctor-bigfiles-threshold")

	found := bigFilesFindings(t, tc, campPath)
	require.Contains(t, found, "media/raw/footage.mov")

	// Raise the limit above the fixture's files; the findings must clear.
	_, err := tc.RunCampInDir(campPath, "settings", "set", "local.commit.guards.max_file_size", "100MiB")
	require.NoError(t, err)

	found = bigFilesFindings(t, tc, campPath)
	assert.NotContains(t, found, "media/raw/footage.mov",
		"raising the guard's threshold must raise doctor's too")
}

// Criterion 31e: camp status never surfaces these findings. A deliberate
// gitignore is not a defect to nag about; doctor is the only surface.
func TestIntegration_DoctorBigFilesNeverAppearsInStatus(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupBigFilesCampaign(t, tc, "doctor-bigfiles-status")

	output, err := tc.RunCampInDir(campPath, "status")
	require.NoError(t, err, "output:\n%s", output)

	assert.NotContains(t, output, "media/raw/footage.mov")
	assert.NotContains(t, output, "owned by no system")
	assert.NotContains(t, output, "bigfiles")
}

// The sweep walks the whole campaign, so it stays out of the default run.
// Every other check is a git invocation costing milliseconds.
func TestIntegration_DoctorBigFilesIsOptIn(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupBigFilesCampaign(t, tc, "doctor-bigfiles-optin")

	stdout, _, _, err := tc.RunCampSplitInDir(campPath, "doctor", "--json")
	require.NoError(t, err)

	var doc doctorJSONDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	for _, issue := range doc.Issues {
		assert.NotEqual(t, "bigfiles", issue.CheckID,
			"bigfiles must not run in the default doctor pass")
	}

	// But it is available by name.
	found := bigFilesFindings(t, tc, campPath)
	assert.NotEmpty(t, found, "-c bigfiles must run the sweep")
}

// Detection never remediates: --fix must change nothing here, because both
// remedies (history rewrite, adopting a directory) are human decisions.
func TestIntegration_DoctorBigFilesFixIsReadOnly(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := setupBigFilesCampaign(t, tc, "doctor-bigfiles-readonly")

	gitignoreBefore := readGitignore(t, tc, campPath)
	declaredBefore, err := tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
	require.NoError(t, err)

	_, _, _, err = tc.RunCampSplitInDir(campPath, "doctor", "-c", "bigfiles", "--fix")
	require.NoError(t, err)

	assert.Equal(t, gitignoreBefore, readGitignore(t, tc, campPath),
		"--fix must not touch .gitignore")
	declaredAfter, err := tc.ReadFile(campPath + "/.campaign/artifacts.yaml")
	require.NoError(t, err)
	assert.Equal(t, declaredBefore, declaredAfter,
		"--fix must not declare anything")

	stillThere := bigFilesFindings(t, tc, campPath)
	assert.Contains(t, stillThere, "media/raw/footage.mov",
		"the finding must survive --fix; this check only reports")
}
