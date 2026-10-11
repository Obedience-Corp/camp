//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type backgroundCommitDoc struct {
	SchemaVersion string `json:"schema_version"`
	Outcome       string `json:"outcome"`
	Repo          string `json:"repo"`
	JobID         string `json:"job_id"`
	Staged        int    `json:"staged"`
	Excluded      []struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	} `json:"excluded"`
	ArtifactRootsDeclared []string `json:"artifact_roots_declared"`
}

func configureBackgroundWriter(t *testing.T, tc *TestContainer, root, name, body string) {
	t.Helper()
	script := "/tmp/" + name + "-writer.sh"
	require.NoError(t, tc.WriteFile(script, "#!/bin/sh\n"+body+"\n"))
	tc.Shell(t, fmt.Sprintf("chmod +x %s\nprintf '\\nhooks:\\n  commit_message:\\n    command: %s\\n' >> %s/.campaign/campaign.yaml", script, script, root))
}

func readBackgroundReceipt(t *testing.T, tc *TestContainer, root string, args ...string) (backgroundCommitDoc, string, int) {
	t.Helper()
	stdout, stderr, code, err := tc.RunCampSplitInDir(root, append([]string{"commit", "--auto-write", "--background", "--json"}, args...)...)
	require.NoError(t, err)
	var doc backgroundCommitDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc), "stdout: %s\nstderr: %s", stdout, stderr)
	assert.Equal(t, "commit-background/v1alpha1", doc.SchemaVersion)
	assert.NotContains(t, stdout, `"commit":`, "a receipt must not pretend to carry a completed commit")
	return doc, stderr, code
}

func TestIntegration_CommitBackgroundJSONQueuesCapturedTree(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupGuardCampaign(t, tc, "background-snapshot")
	release := "/tmp/background-snapshot-release"
	// A bounded gate proves ordering without a wall-clock speed assertion.
	// If commit incorrectly waits inline, the writer fails instead of hanging.
	configureBackgroundWriter(t, tc, root, "background-snapshot", fmt.Sprintf(`
for i in $(seq 1 200); do
 if [ -f %s ]; then echo 'background captured content'; exit 0; fi
 sleep 0.1
done
exit 1`, release))
	tc.EnableDeferral()
	doc, stderr, code := readBackgroundReceipt(t, tc, root)
	require.Equal(t, 0, code, "%s", stderr)
	require.Equal(t, "queued", doc.Outcome)
	require.NotEmpty(t, doc.JobID)
	assert.Equal(t, root, doc.Repo)
	assert.Greater(t, doc.Staged, 0)
	assert.NotNil(t, doc.Excluded)
	assert.NotNil(t, doc.ArtifactRootsDeclared)
	jobs, err := tc.RunCampInDir(root, "jobs", "--json")
	require.NoError(t, err, "%s", jobs)
	assert.Contains(t, jobs, doc.JobID, "receipt identity must name a real outstanding job")
	assert.Contains(t, jobs, `"kind": "commit-tree"`)
	require.NoError(t, tc.WriteFile(root+"/notes/one.md", "edited after enqueue"))
	require.NoError(t, tc.WriteFile(release, "release"))
	out, err := tc.RunCampInDir(root, "jobs", "drain")
	require.NoError(t, err, "%s", out)
	assert.Equal(t, "first note", strings.TrimSpace(tc.GitOutput(t, root, "show", "HEAD:notes/one.md")))
	assert.Contains(t, tc.GitOutput(t, root, "diff", "--", "notes/one.md"), "edited after enqueue")
	assert.Contains(t, tc.GitOutput(t, root, "log", "-1", "--format=%s"), "background captured content")
}

func TestIntegration_CommitBackgroundUnavailableWriterStaysVisible(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupGuardCampaign(t, tc, "background-failure")
	configureBackgroundWriter(t, tc, root, "background-failure", "echo writer-unavailable >&2\nexit 75")
	tc.EnableDeferral()
	doc, stderr, code := readBackgroundReceipt(t, tc, root)
	require.Equal(t, 0, code, "%s", stderr)
	require.Equal(t, "queued", doc.Outcome)
	require.Eventually(t, func() bool {
		out, err := tc.RunCampInDir(root, "jobs", "--json")
		return err == nil && strings.Contains(out, doc.JobID) && strings.Contains(out, `"writer_attempts": 1`)
	}, 15*time.Second, 100*time.Millisecond)
}

func TestIntegration_CommitBackgroundJSONPreservesExclusions(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupMixedRootCampaign(t, tc, "background-exclusions")
	configureBackgroundWriter(t, tc, root, "background-exclusions", "echo 'capture small files'")
	tc.EnableDeferral()
	doc, stderr, code := readBackgroundReceipt(t, tc, root)
	require.Equal(t, 0, code, "%s", stderr)
	require.Equal(t, "queued", doc.Outcome)
	require.Len(t, doc.Excluded, 1)
	assert.Equal(t, "videos/my-video/footage.mp4", doc.Excluded[0].Path)
	assert.Equal(t, "size_guard", doc.Excluded[0].Reason)
	assert.Equal(t, []string{"videos/my-video"}, doc.ArtifactRootsDeclared)
	out, err := tc.RunCampInDir(root, "jobs", "drain")
	require.NoError(t, err, "%s", out)
	assert.NotContains(t, tc.GitOutput(t, root, "ls-tree", "-r", "--name-only", "HEAD"), "footage.mp4")
}

func TestIntegration_CommitBackgroundJSONNoop(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupGuardCampaign(t, tc, "background-noop")
	configureBackgroundWriter(t, tc, root, "background-noop", "echo unused")
	_, err := tc.RunCampInDir(root, "commit", "-m", "baseline")
	require.NoError(t, err)
	tc.EnableDeferral()
	doc, stderr, code := readBackgroundReceipt(t, tc, root)
	require.Equal(t, 0, code, "%s", stderr)
	assert.Equal(t, "nothing_to_commit", doc.Outcome)
	assert.Empty(t, doc.JobID)
	assert.Zero(t, doc.Staged)
	assert.Equal(t, root, doc.Repo)
}

func TestIntegration_CommitAutoWriteJSONStillSynchronous(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupGuardCampaign(t, tc, "background-sync-contract")
	configureBackgroundWriter(t, tc, root, "background-sync-contract", "echo 'synchronous writer'")
	tc.EnableDeferral()
	_, doc := runCommitJSON(t, tc, root, "--auto-write")
	assert.Equal(t, "commit/v1alpha1", doc.SchemaVersion)
	assert.True(t, doc.OK)
	assert.Equal(t, strings.TrimSpace(tc.GitOutput(t, root, "rev-parse", "HEAD")), doc.Commit)
	assert.Contains(t, tc.GitOutput(t, root, "log", "-1", "--format=%s"), "synchronous writer")
}

func TestIntegration_CommitBackgroundRefusesUnsafeModes(t *testing.T) {
	for _, mode := range []string{"manual", "amend", "message", "disabled", "hook", "missing-writer", "queue-error"} {
		t.Run(mode, func(t *testing.T) {
			tc := GetSharedContainer(t)
			root := setupGuardCampaign(t, tc, "background-refuse-"+mode)
			if mode != "missing-writer" {
				configureBackgroundWriter(t, tc, root, "background-refuse-"+mode, "touch /tmp/should-not-run-"+mode+"\necho unexpected")
			}
			if mode != "disabled" {
				tc.EnableDeferral()
			}
			args := []string{"commit", "--background", "--json", "--auto-write"}
			switch mode {
			case "manual":
				args = args[:3]
			case "amend":
				args = append(args, "--amend")
			case "message":
				args = append(args, "-m", "unexpected")
			case "hook":
				tc.Shell(t, fmt.Sprintf("printf '#!/bin/sh\\nexit 0\\n' > %s/.git/hooks/pre-commit; chmod +x %s/.git/hooks/pre-commit", root, root))
			case "queue-error":
				tc.Shell(t, fmt.Sprintf("mkdir -p %s/.campaign/cache/jobs; touch %s/.campaign/cache/jobs/pending", root, root))
				args = append(args, "--no-drain")
			}
			head := tc.GitOutput(t, root, "rev-parse", "HEAD")
			stdout, stderr, code, err := tc.RunCampSplitInDir(root, args...)
			require.NoError(t, err)
			require.NotZero(t, code, "stdout: %s\nstderr: %s", stdout, stderr)
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, `"schema_version": "commit-background/v1alpha1"`)
			assert.Equal(t, head, tc.GitOutput(t, root, "rev-parse", "HEAD"))
			if mode != "queue-error" {
				assert.Empty(t, stagedPaths(t, tc, root), "preflight refusal must precede staging")
			}
			tc.Shell(t, "test ! -e /tmp/should-not-run-"+mode)
		})
	}
}

func TestIntegration_CommitBackgroundJSONGuardRefusal(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupGuardCampaign(t, tc, "background-guard-refusal")
	configureBackgroundWriter(t, tc, root, "background-guard-refusal", "echo unused")
	writeGuardConfig(t, tc, root, "    max_file_size: 1MiB\n    large_files: block")
	tc.Shell(t, fmt.Sprintf("dd if=/dev/zero of=%s/large.dat bs=1024 count=3072 2>/dev/null", root))
	tc.EnableDeferral()
	doc, stderr, code := readBackgroundReceipt(t, tc, root)
	require.NotZero(t, code)
	assert.Equal(t, "refused", doc.Outcome)
	assert.Empty(t, doc.JobID)
	require.Len(t, doc.Excluded, 1)
	assert.Equal(t, "size_guard_blocked", doc.Excluded[0].Reason)
	assert.Contains(t, stderr, `"schema_version": "commit-background/v1alpha1"`)
	assert.Empty(t, stagedPaths(t, tc, root))
}

func TestIntegration_CommitBackgroundConflictStaysFailed(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupGuardCampaign(t, tc, "background-conflict")
	release := "/tmp/background-conflict-release"
	configureBackgroundWriter(t, tc, root, "background-conflict", fmt.Sprintf(`
for i in $(seq 1 200); do
 if [ -f %s ]; then echo 'captured edit'; exit 0; fi
 sleep 0.1
done
exit 1`, release))
	out, err := tc.RunCampInDir(root, "commit", "-m", "baseline")
	require.NoError(t, err, "%s", out)
	require.NoError(t, tc.WriteFile(root+"/notes/one.md", "captured edit\n"))
	require.NoError(t, tc.WriteFile(root+"/notes/two.md", "captured second edit\n"))
	tc.EnableDeferral()
	doc, stderr, code := readBackgroundReceipt(t, tc, root)
	require.Equal(t, 0, code, "%s", stderr)
	require.NotEmpty(t, doc.JobID)
	require.NoError(t, tc.WriteFile(root+"/notes/one.md", "competing edit\n"))
	tc.GitOutput(t, root, "reset", "HEAD")
	tc.GitOutput(t, root, "add", "notes/one.md")
	out, err = tc.RunCampInDir(root, "commit", "--all=false", "--no-drain", "-m", "competing commit")
	require.NoError(t, err, "%s", out)
	head := tc.GitOutput(t, root, "rev-parse", "HEAD")
	require.NoError(t, tc.WriteFile(release, "release"))
	var jobOutput string
	landed := assert.Eventually(t, func() bool {
		jobOutput, err = tc.RunCampInDir(root, "jobs", "--json")
		return err == nil && strings.Contains(jobOutput, doc.JobID) && strings.Contains(jobOutput, `"state": "failed"`)
	}, 15*time.Second, 100*time.Millisecond)
	require.True(t, landed, "last jobs output: %s", jobOutput)
	assert.Equal(t, head, tc.GitOutput(t, root, "rev-parse", "HEAD"), "a failed job must preserve the competing commit")
}

func TestIntegration_CommitBackgroundJSONAllExcluded(t *testing.T) {
	tc := GetSharedContainer(t)
	root := setupGuardCampaign(t, tc, "background-all-excluded")
	configureBackgroundWriter(t, tc, root, "background-all-excluded", "echo unused")
	writeGuardConfig(t, tc, root, "    max_file_size: 1MiB")
	out, err := tc.RunCampInDir(root, "commit", "-m", "baseline")
	require.NoError(t, err, "%s", out)
	tc.Shell(t, fmt.Sprintf("dd if=/dev/zero of=%s/large.dat bs=1024 count=3072 2>/dev/null", root))
	tc.EnableDeferral()
	doc, stderr, code := readBackgroundReceipt(t, tc, root)
	require.Equal(t, 0, code, "%s", stderr)
	assert.Equal(t, "nothing_to_commit", doc.Outcome)
	assert.Empty(t, doc.JobID)
	require.Len(t, doc.Excluded, 1)
	assert.Equal(t, "large.dat", doc.Excluded[0].Path)
	assert.Equal(t, "needs_root_decision", doc.Excluded[0].Reason)
}
