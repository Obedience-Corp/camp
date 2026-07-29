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

// Deferring an --auto-write commit means the commit does not exist when the
// user's terminal returns. Every property below is about what the eventual
// commit contains, which no package test can observe: the whole point is what
// happens to a real repository in the gap.

// configureWriter points hooks.commit_message.command at a script.
//
// The script is a file rather than an inline command so a test can make it slow
// or make it fail without fighting shell quoting through the config.
func configureWriter(t *testing.T, tc *TestContainer, campPath, script string) {
	t.Helper()
	tc.Shell(t, fmt.Sprintf(`
		mkdir -p /writers
		cat > /writers/%s.sh <<'SCRIPT'
%s
SCRIPT
		chmod +x /writers/%s.sh
		cd %s
		cat >> .campaign/campaign.yaml <<'YAML'
hooks:
  commit_message:
    command: /writers/%s.sh
YAML
	`, script, writerScripts[script], script, campPath, script))
}

// writerScripts are the message writers the tests use. Each prints a message on
// stdout, which is the whole contract.
var writerScripts = map[string]string{
	"plain": `#!/bin/sh
echo "deferred: written by the background writer"`,
	"slow": `#!/bin/sh
sleep 3
echo "deferred: the slow writer finished"`,
	"empty": `#!/bin/sh
exit 0`,
}

// drainJobs waits for the queue so an assertion does not race the worker.
func drainJobs(t *testing.T, tc *TestContainer, campPath string) {
	t.Helper()
	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "jobs", "drain")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
}

// headSubject returns the subject line of HEAD.
func headSubject(t *testing.T, tc *TestContainer, campPath string) string {
	t.Helper()
	return strings.TrimSpace(tc.GitOutput(t, campPath, "log", "-1", "--format=%s"))
}

// Deferred criterion 2b: staging more files after the enqueue does not change
// the queued commit.
//
// The captured tree is the snapshot. If the worker ran a plain `git commit`
// later it would sweep in whatever the index held by then, which is exactly the
// class of surprise deferral must not introduce.
func TestIntegration_DeferredCommitIgnoresLaterStaging(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-2b")
	configureWriter(t, tc, campPath, "plain")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'first\n' > first.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Contains(t, stdout+stderr, "staged and queued",
		"a deferred commit must say it queued rather than committed; output:\n%s", stdout+stderr)

	// The user keeps working: a second file, staged, in the gap.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'second\n' > second.md
		git add second.md
	`, campPath))

	drainJobs(t, tc, campPath)

	committed := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", "HEAD")
	assert.Contains(t, committed, "first.md", "the queued commit must contain what was staged when it queued")
	assert.NotContains(t, committed, "second.md",
		"the queued commit must not sweep in work staged after it; commit contained:\n%s", committed)
}

// Deferred criterion 2c: editing a file in the gap changes neither the queued
// commit nor the user's edit.
func TestIntegration_DeferredCommitIgnoresLaterEdits(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-2c")
	configureWriter(t, tc, campPath, "plain")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'as staged\n' > note.md
	`, campPath))

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'edited after queueing\n' > note.md
	`, campPath))

	drainJobs(t, tc, campPath)

	committed := strings.TrimSpace(tc.GitOutput(t, campPath, "show", "HEAD:note.md"))
	assert.Equal(t, "as staged", committed,
		"the commit must carry the staged content, not the later edit")

	onDisk, err := tc.ReadFile(campPath + "/note.md")
	require.NoError(t, err)
	assert.Equal(t, "edited after queueing", strings.TrimSpace(onDisk),
		"the user's edit must survive untouched")

	status := tc.GitOutput(t, campPath, "status", "--porcelain", "note.md")
	assert.Contains(t, status, "note.md",
		"the later edit must be left uncommitted, not silently absorbed")
}

// Deferred criterion 2d: if HEAD moved since the enqueue, the job fails and
// says so. It never rebases.
//
// Replaying a queued commit onto someone else's commit would produce a tree
// nobody chose. Failing is the only honest outcome, and the failure has to be
// visible rather than silent.
func TestIntegration_DeferredCommitFailsWhenHeadMoved(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-2d")
	configureWriter(t, tc, campPath, "slow")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'queued\n' > queued.md
	`, campPath))

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	// Someone commits directly while the writer is still running.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'interloper\n' > interloper.md
		git add interloper.md
		git commit -q -m "committed directly while the job was queued"
	`, campPath))

	beforeDrain := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))

	// The drain reports the failure rather than hanging: the job leaves
	// pending/, so the wait ends.
	_, _, _, err = tc.RunCampSplitInDir(campPath, "jobs", "drain")
	require.NoError(t, err)

	afterDrain := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))
	assert.Equal(t, beforeDrain, afterDrain,
		"a job whose parent moved must not touch HEAD")

	subject := headSubject(t, tc, campPath)
	assert.Contains(t, subject, "committed directly",
		"the interloping commit must still be HEAD; camp must never rebase over it")

	// The failure is visible, both in the queue and on the next command.
	stdout, _, _, err := tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	assert.Contains(t, stdout, "failed",
		"the job must be parked as failed, not vanish; camp jobs:\n%s", stdout)

	_, stderr, _, err = tc.RunCampSplitInDir(campPath, "status", "--short")
	require.NoError(t, err)
	assert.Contains(t, stderr, "deferred commit failed",
		"the failure must surface on an ordinary command; stderr:\n%s", stderr)
}

// Deferred criterion 37c2: `camp commit -m` stays synchronous and prints a
// hash. Only --auto-write and camp's own bookkeeping defer.
func TestIntegration_MessageCommitStaysSynchronous(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-37c2")
	configureWriter(t, tc, campPath, "plain")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'sync\n' > sync.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "-m", "an ordinary commit")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	// Contains, not Equal: camp prepends a campaign tag to the subject.
	assert.Contains(t, headSubject(t, tc, campPath), "an ordinary commit",
		"a -m commit must have landed by the time the command returns")
	assert.NotContains(t, stdout+stderr, "staged and queued",
		"a -m commit must not defer")

	stdout, _, _, err = tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	assert.Contains(t, stdout, "No deferred commits queued",
		"a -m commit must create no job; camp jobs:\n%s", stdout)
}

// Deferred criterion 37c3: --auto-write returns before the writer runs. That is
// the entire user-visible point, so it is asserted against a writer that takes
// measurably longer than the command.
func TestIntegration_AutoWriteReturnsBeforeTheWriterFinishes(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-37c3")
	configureWriter(t, tc, campPath, "slow")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'quick\n' > quick.md
	`, campPath))

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	// The writer sleeps 3s. Immediately after the command returns, the commit
	// must not exist yet: HEAD still has whatever was there before.
	subject := headSubject(t, tc, campPath)
	assert.NotContains(t, subject, "the slow writer finished",
		"--auto-write returned only after the writer ran; the terminal was held")

	drainJobs(t, tc, campPath)
	assert.Contains(t, headSubject(t, tc, campPath), "the slow writer finished",
		"the deferred commit must eventually land with the writer's message")
}

// A repository with commit hooks keeps today's synchronous behavior exactly.
//
// A hook is the user's own code, expecting to run at commit time against the
// tree being committed. Deferring past it would either skip it or run it later
// against a different working tree, and both are worse than not deferring.
func TestIntegration_HookRepoCommitsInTheForeground(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-hooks")
	configureWriter(t, tc, campPath, "plain")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		cat > .git/hooks/pre-commit <<'HOOK'
#!/bin/sh
echo "the hook ran" > /tmp/hook-sentinel
HOOK
		chmod +x .git/hooks/pre-commit
		rm -f /tmp/hook-sentinel
		printf 'hooked\n' > hooked.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)

	assert.NotContains(t, stdout+stderr, "staged and queued",
		"a repository with commit hooks must not defer; output:\n%s", stdout+stderr)
	assert.Contains(t, headSubject(t, tc, campPath), "deferred: written by the background writer",
		"the commit must have landed synchronously")

	ran, err := tc.CheckFileExists("/tmp/hook-sentinel")
	require.NoError(t, err)
	assert.True(t, ran, "the user's pre-commit hook must have run")

	stdout, _, _, err = tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	assert.Contains(t, stdout, "No deferred commits queued",
		"a hook repository must create no job; camp jobs:\n%s", stdout)
}

// A non-executable hook does not force the whole repository back to
// synchronous commits: git itself would not run it either.
func TestIntegration_NonExecutableHookDoesNotBlockDeferral(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-hooks-inert")
	configureWriter(t, tc, campPath, "plain")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf '#!/bin/sh\nexit 0\n' > .git/hooks/pre-commit
		chmod -x .git/hooks/pre-commit
		printf 'inert\n' > inert.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)
	assert.Contains(t, stdout+stderr, "staged and queued",
		"a hook git would not run must not disable deferral; output:\n%s", stdout+stderr)
}

// CAMP_NO_DEFER is the escape hatch for harnesses and agents that need strict
// determinism. It must give exactly the pre-deferral behavior.
func TestIntegration_CampNoDeferForcesInlineCommits(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-nodefer")
	configureWriter(t, tc, campPath, "plain")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'inline\n' > inline.md
	`, campPath))

	stdout := tc.Shell(t, fmt.Sprintf(
		"cd %s && CAMP_NO_DEFER=1 /camp commit --auto-write 2>&1", campPath))

	assert.NotContains(t, stdout, "staged and queued",
		"CAMP_NO_DEFER must force the inline path; output:\n%s", stdout)
	assert.Contains(t, headSubject(t, tc, campPath), "deferred: written by the background writer",
		"the commit must have landed before the command returned")
}

// A deferred commit must carry the workitem context its synchronous twin would
// have. The worker is a detached child with no meaningful working directory, so
// anything not recorded on the job at enqueue is simply lost, and a deferred
// commit would silently drop the WI- tag the foreground one carries.
func TestIntegration_DeferredCommitCarriesWorkitemEnv(t *testing.T) {
	tc := GetSharedContainer(t)
	dir := "/test/autowrite-defer-env"
	initCommitTagsCampaign(t, tc, dir)
	ref := seedDesignWorkitemWithRef(t, tc, dir, "envcarry")

	// A writer that reports what camp handed it, so the assertion is about the
	// environment the writer actually saw rather than about camp's own tagging.
	require.NoError(t, tc.WriteFile("/tmp/defer_env_hook.sh",
		"#!/bin/sh\necho \"deferred with WI=${CAMP_WORKITEM_REF:-none}\"\n"))
	_, _, chmodErr := tc.ExecCommand("chmod", "+x", "/tmp/defer_env_hook.sh")
	require.NoError(t, chmodErr)

	hookYAML := "\nhooks:\n  commit_message:\n    command: /tmp/defer_env_hook.sh\n"
	_, _, hookErr := tc.ExecCommand("sh", "-c",
		"cat >> "+dir+"/.campaign/campaign.yaml <<EOF"+hookYAML+"EOF")
	require.NoError(t, hookErr)

	wiDir := dir + "/workflow/design/envcarry"
	out, err := tc.RunCampInDir(wiDir, "commit", "--auto-write")
	require.NoError(t, err, "camp commit --auto-write: %s", out)
	require.Contains(t, out, "staged and queued",
		"this test is about the deferred path; it did not defer:\n%s", out)

	drainJobs(t, tc, dir)

	subject := headSubject(t, tc, dir)
	assert.NotContains(t, subject, "WI=none",
		"the writer ran without CAMP_WORKITEM_REF; subject: %s", subject)
	assert.Contains(t, subject, ref,
		"the deferred commit lost the workitem the foreground path would have tagged; subject: %s", subject)
}

// A writer that produces nothing fails the job rather than committing an empty
// message. The queue keeps the failure as evidence.
func TestIntegration_EmptyWriterOutputFailsTheJob(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-empty")
	configureWriter(t, tc, campPath, "empty")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'nomessage\n' > nomessage.md
	`, campPath))

	before := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	_, _, _, err = tc.RunCampSplitInDir(campPath, "jobs", "drain")
	require.NoError(t, err)

	after := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))
	assert.Equal(t, before, after, "no commit may be created without a message")

	stdout, _, _, err := tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	assert.Contains(t, stdout, "failed",
		"the failure must be kept as evidence; camp jobs:\n%s", stdout)
}
