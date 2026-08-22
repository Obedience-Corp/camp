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
	"stable-head": `#!/bin/sh
before=$(git diff --cached | git hash-object --stdin)
touch /tmp/camp-writer-stable-ready
i=0
while [ ! -e /tmp/camp-writer-stable-release ] && [ "$i" -lt 100 ]; do
  sleep 0.1
  i=$((i + 1))
done
after=$(git diff --cached | git hash-object --stdin)
if [ "$before" != "$after" ]; then
  echo "writer saw its staged input change" >&2
  exit 91
fi
echo "deferred: stable captured input"`,
	"empty": `#!/bin/sh
exit 0`,
	// A writer whose backing daemon is down prints usage and exits non-zero.
	// The job must fail with that evidence; camp must not invent a subject.
	"broken": `#!/bin/sh
echo "Usage:" >&2
echo "  writer [flags]" >&2
echo "connect to daemon: writer: daemon not running" >&2
exit 1`,
}

// commitWithScratchIndex advances HEAD without sweeping the real index, which
// still contains a deferred commit's captured tree. It models an independent
// history writer rather than the common "git add -A" case that carries the
// queued content along with its own commit.
func commitWithScratchIndex(t *testing.T, tc *TestContainer, campPath, filename string) {
	t.Helper()
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		index=/tmp/camp-autowrite-%s.index
		rm -f "$index"
		GIT_INDEX_FILE="$index" git read-tree HEAD
		printf 'interloper\n' > %s
		GIT_INDEX_FILE="$index" git add -- %s
		GIT_INDEX_FILE="$index" git commit -q -m "committed independently while the job was queued"
		rm -f "$index"
	`, campPath, filename, filename, filename))
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
	tc.EnableDeferral()
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
	tc.EnableDeferral()
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
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-2d")
	configureWriter(t, tc, campPath, "slow")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'queued\n' > queued.md
	`, campPath))

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	// Someone commits independently while the writer is still running. A
	// scratch index is essential here: an ordinary commit would sweep the
	// queued paths that Camp deliberately leaves staged, fulfilling the job.
	commitWithScratchIndex(t, tc, campPath, "interloper.md")

	beforeDrain := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))

	// The drain reports the failure rather than hanging: the job leaves
	// pending/, so the wait ends.
	_, _, _, err = tc.RunCampSplitInDir(campPath, "jobs", "drain")
	require.NoError(t, err)

	afterDrain := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))
	assert.Equal(t, beforeDrain, afterDrain,
		"a job whose parent moved must not touch HEAD")

	subject := headSubject(t, tc, campPath)
	assert.Contains(t, subject, "committed independently",
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

// A later commit that sweeps the still-staged queued paths fulfills the job.
// This is the real shared-campaign case: an auto-written message can take
// minutes, and another Camp/Fest operation may commit all staged content while
// it runs. The writer must keep seeing the captured parent, and the worker must
// not park a false failure merely because the later commit also added files.
func TestIntegration_DeferredCommitAcceptsSnapshotSweptByLaterCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-swept")
	configureWriter(t, tc, campPath, "stable-head")
	tc.Shell(t, "rm -f /tmp/camp-writer-stable-ready /tmp/camp-writer-stable-release")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'queued\n' > queued.md
		: > ephemeral.lock
	`, campPath))

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	// Wait until the writer has captured its first digest, then advance live
	// HEAD with the real index. The first commit carries a newer queued.md plus
	// its own file; the second removes another queued path. No history entry has
	// the exact queued projection, but every path was versioned and the index is
	// settled, which is the supersession contract this regression exercises.
	tc.Shell(t, `
		i=0
		while [ ! -e /tmp/camp-writer-stable-ready ] && [ "$i" -lt 100 ]; do
		  sleep 0.1
		  i=$((i + 1))
		done
		test -e /tmp/camp-writer-stable-ready
	`)
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'later\n' >> queued.md
		printf 'later\n' > later.md
		git add queued.md later.md
		git commit -q -m "later commit swept the queued snapshot"
		git rm -q ephemeral.lock
		git commit -q -m "later commit removed an obsolete queued path"
		touch /tmp/camp-writer-stable-release
	`, campPath))

	drainJobs(t, tc, campPath)
	jobsOut, _, _, err := tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	assert.Contains(t, jobsOut, "No deferred commits queued",
		"a later commit containing the captured paths fulfills the job; camp jobs:\n%s", jobsOut)

	queued := tc.GitOutput(t, campPath, "show", "HEAD:queued.md")
	assert.Contains(t, queued, "queued")
	assert.Contains(t, queued, "later")
	tree := tc.GitOutput(t, campPath, "ls-tree", "-r", "--name-only", "HEAD")
	assert.Contains(t, tree, "later.md")
	assert.NotContains(t, tree, "ephemeral.lock")
}

// Deferred criterion 37c2: `camp commit -m` stays synchronous and prints a
// hash. Only --auto-write and camp's own bookkeeping defer.
func TestIntegration_MessageCommitStaysSynchronous(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
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
	tc.EnableDeferral()
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
	tc.EnableDeferral()
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
	tc.EnableDeferral()
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
	tc.EnableDeferral()
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
	tc.EnableDeferral()
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
	tc.EnableDeferral()
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

// A writer outage fails the job. Camp does not invent a commit message: a
// filler subject in history is worse than a parked job the user can retry or
// drop once the writer is healthy.
func TestIntegration_WriterFailureFailsTheJob(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-broken")
	configureWriter(t, tc, campPath, "broken")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'rescued\n' > rescued.md
	`, campPath))

	before := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	_, _, _, err = tc.RunCampSplitInDir(campPath, "jobs", "drain")
	require.NoError(t, err)

	after := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))
	assert.Equal(t, before, after,
		"a writer failure must not land a filler commit; HEAD moved")

	stdout, _, _, err := tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	assert.Contains(t, stdout, "failed",
		"the failure must be kept as evidence; camp jobs:\n%s", stdout)
	assert.NotContains(t, stdout, "writer unavailable",
		"camp must not mint a filler subject; camp jobs:\n%s", stdout)
}

// An empty staged tree must not enqueue a deferred commit. Campaign-root
// commits exclude submodule refs, so only-submodule dirt used to pass
// HasChanges, write-tree equal to HEAD, then land a no-op with a filler
// message after the writer correctly reported "no staged changes".
// `camp init` leaves HEAD unborn until the first commit. A deferred
// --auto-write used to fail git.FullHash, print "Could not queue this commit",
// and fall back to a synchronous write. Empty Parent is a valid KindCommitTree
// snapshot, so the first --auto-write must enqueue and land as a root commit.
func TestIntegration_AutoWriteDefersOnUnbornHead(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()

	campPath := "/campaigns/aw-defer-unborn"
	_, err := tc.RunCamp("init", campPath, "--name", "aw-defer-unborn",
		"-d", "Test campaign", "-m", "Test mission", "--type", "product")
	require.NoError(t, err)

	_, exitCode, err := tc.ExecCommand("git", "-C", campPath, "rev-parse", "--verify", "HEAD")
	require.NoError(t, err)
	require.NotEqual(t, 0, exitCode, "fixture is not reproducing an unborn HEAD; HEAD already resolves")

	configureWriter(t, tc, campPath, "plain")
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'first auto-write\n' > first.md
	`, campPath))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.NotContains(t, stdout+stderr, "Could not queue this commit",
		"unborn HEAD must not warn about a queue failure; output:\n%s", stdout+stderr)
	assert.Contains(t, stdout+stderr, "queued",
		"unborn-HEAD --auto-write must enqueue; output:\n%s", stdout+stderr)

	drainJobs(t, tc, campPath)

	assert.Zero(t, pendingJobCount(t, tc, campPath, "failed", rootLane),
		"the deferred --auto-write must not land in the failed lane on an unborn HEAD")

	head := tc.GitOutput(t, campPath, "rev-parse", "HEAD")
	assert.NotEmpty(t, head, "the deferred job must create the repository's first commit")

	count := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-list", "--count", "HEAD"))
	assert.Equal(t, "1", count, "the worker must produce exactly one commit, got %s", count)

	parents := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-list", "--parents", "-n", "1", "HEAD"))
	assert.Len(t, strings.Fields(parents), 1, "the first commit must be a root commit; got %q", parents)

	tracked := tc.GitOutput(t, campPath, "ls-tree", "-r", "--name-only", "HEAD")
	assert.Contains(t, tracked, "first.md",
		"the first commit must contain the staged file; tree:\n%s", tracked)

	assert.Contains(t, headSubject(t, tc, campPath), "deferred: written by the background writer",
		"the deferred commit must carry the writer's message")
}

// A deferred --auto-write captured on an unborn HEAD (empty Job.Parent) must
// fail cleanly, with the same "HEAD moved" contract as a born-HEAD job, when
// someone else creates the repository's actual first commit while the writer
// is still running and that commit does not carry the queued paths.
//
// Regression: FirstParentChainContainsOrSupersedesTreeChanges diffed an empty
// Parent as a literal empty-string git revision, which git rejects as an
// ambiguous argument, so the worker surfaced that raw plumbing error instead
// of the "HEAD is no longer unborn" message every other HEAD-moved failure
// gets.
func TestIntegration_DeferredCommitFailsWhenUnbornHeadRaced(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()

	campPath := "/campaigns/aw-defer-unborn-race"
	_, err := tc.RunCamp("init", campPath, "--name", "aw-defer-unborn-race",
		"-d", "Test campaign", "-m", "Test mission", "--type", "product")
	require.NoError(t, err)

	_, exitCode, err := tc.ExecCommand("git", "-C", campPath, "rev-parse", "--verify", "HEAD")
	require.NoError(t, err)
	require.NotEqual(t, 0, exitCode, "fixture is not reproducing an unborn HEAD; HEAD already resolves")

	configureWriter(t, tc, campPath, "slow")
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'queued\n' > queued.md
	`, campPath))

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	// Someone else creates the repository's real first commit independently
	// while the writer is still running, via a scratch index so the queued
	// paths stay staged and out of that commit's tree.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		index=/tmp/camp-autowrite-unborn-race.index
		rm -f "$index"
		printf 'interloper\n' > interloper.md
		GIT_INDEX_FILE="$index" git add -- interloper.md
		GIT_INDEX_FILE="$index" git commit -q -m "committed independently before the queued job landed"
		rm -f "$index"
	`, campPath))

	_, _, _, err = tc.RunCampSplitInDir(campPath, "jobs", "drain")
	require.NoError(t, err)

	head := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))
	assert.NotEmpty(t, head, "the interloper's commit must still exist")

	subject := headSubject(t, tc, campPath)
	assert.Contains(t, subject, "committed independently",
		"the interloping commit must still be HEAD; camp must never rebase over it")

	jobsOut, _, _, err := tc.RunCampSplitInDir(campPath, "jobs", "--json")
	require.NoError(t, err)
	assert.Contains(t, jobsOut, "HEAD is no longer unborn",
		"the failure must use the unborn-HEAD-moved message, not a raw git error; camp jobs --json:\n%s", jobsOut)
	assert.NotContains(t, jobsOut, "ambiguous argument",
		"an empty Parent must not reach git as a literal revision string; camp jobs --json:\n%s", jobsOut)
}

func TestIntegration_EmptyStagedTreeDoesNotDefer(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-empty-tree")
	configureWriter(t, tc, campPath, "plain")
	// Commit the writer config itself so the second --auto-write sees a clean
	// index (tree == HEAD), which is the empty-snapshot case under test.
	tc.Shell(t, fmt.Sprintf(`
		cd %s
		git add .campaign/campaign.yaml
		git commit -q -m "configure the message writer"
	`, campPath))

	before := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))

	stdout, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.NotContains(t, stdout, "queued",
		"empty snapshot must not be deferred; output:\n%s", stdout)
	assert.Contains(t, stdout, "Nothing to commit",
		"empty snapshot must report a no-op; output:\n%s", stdout)

	after := strings.TrimSpace(tc.GitOutput(t, campPath, "rev-parse", "HEAD"))
	assert.Equal(t, before, after, "no commit may land from an empty tree")

	jobsOut, _, _, err := tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	assert.NotContains(t, jobsOut, "pending",
		"empty snapshot must not enqueue; camp jobs:\n%s", jobsOut)
	assert.NotContains(t, jobsOut, "failed",
		"empty snapshot must not leave a failed job; camp jobs:\n%s", jobsOut)
}

// A failed job that can never be retried must not be advertised as retryable.
//
// The queue's advice is the only thing standing between a user and a loop with
// no exit: retrying a job whose parent moved fails identically every time.
func TestIntegration_SupersededJobIsNotOfferedForRetry(t *testing.T) {
	tc := GetSharedContainer(t)
	tc.EnableDeferral()
	campPath, _ := setupDrainCampaign(t, tc, "aw-defer-superseded")
	configureWriter(t, tc, campPath, "slow")

	tc.Shell(t, fmt.Sprintf(`
		cd %s
		printf 'queued\n' > queued.md
	`, campPath))

	_, stderr, exitCode, err := tc.RunCampSplitInDir(campPath, "commit", "--auto-write")
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, "stderr:\n%s", stderr)

	// Move HEAD without carrying the queued snapshot, so the job is genuinely
	// superseded rather than already fulfilled by the later commit.
	commitWithScratchIndex(t, tc, campPath, "superseding.md")

	_, _, _, err = tc.RunCampSplitInDir(campPath, "jobs", "drain")
	require.NoError(t, err)

	stdout, _, _, err := tc.RunCampSplitInDir(campPath, "jobs")
	require.NoError(t, err)
	assert.Contains(t, stdout, "cannot retry",
		"the row must say the job is beyond retry; camp jobs:\n%s", stdout)
	assert.NotContains(t, stdout, "camp jobs retry all",
		"camp must not name a command that cannot work; camp jobs:\n%s", stdout)
	assert.Contains(t, stdout, "camp jobs drop",
		"the listing must name the action that does work; camp jobs:\n%s", stdout)

	// Agents read the queue as JSON and need the same fact without parsing prose.
	jsonOut, _, _, err := tc.RunCampSplitInDir(campPath, "jobs", "--json")
	require.NoError(t, err)
	assert.Contains(t, jsonOut, `"superseded": true`,
		"--json must carry the retryability of a failed job; output:\n%s", jsonOut)
}
