//go:build integration
// +build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Committed artifact manifests, criteria 16, 17, 19, 21, 21b, 21c, and 27.
// Every camp invocation pins CAMP_MACHINE_NAME so the manifest identity is
// deterministic regardless of the container's hostname, and jobs are served
// by an explicit worker run under the same identity.

func campAs(t *testing.T, tc *TestContainer, machine, campPath, args string) string {
	t.Helper()
	return tc.Shell(t, fmt.Sprintf(
		`cd %s && CAMP_MACHINE_NAME=%s /camp %s 2>&1 || true`, campPath, machine, args))
}

// settleJobs serves every queued job under one identity and waits for the
// queue to empty, so a later phase can switch identity without an earlier
// phase's worker picking up its jobs.
func settleJobs(t *testing.T, tc *TestContainer, machine, campPath string) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for {
		campAs(t, tc, machine, campPath, "jobs run --campaign "+campPath)
		left := strings.TrimSpace(tc.Shell(t, fmt.Sprintf(
			`find %s/.campaign/cache/jobs/pending %s/.campaign/cache/jobs/running -name '*.json' 2>/dev/null | wc -l`,
			campPath, campPath)))
		if left == "0" || left == "" {
			failed := strings.TrimSpace(tc.Shell(t, fmt.Sprintf(
				`find %s/.campaign/cache/jobs/failed -name '*.json' 2>/dev/null | wc -l`, campPath)))
			if failed != "0" && failed != "" {
				log := tc.Shell(t, fmt.Sprintf(
					`tail -20 %s/.campaign/cache/jobs/worker.log 2>/dev/null; find %s/.campaign/cache/jobs/failed -name '*.json' -exec cat {} \;`,
					campPath, campPath))
				t.Fatalf("jobs failed while settling:\n%s", log)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("queue did not settle; %s jobs left", left)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// shaBySubject resolves the commit whose subject matches exactly, so a test
// never mistakes an asynchronously landed manifest carrier for the commit it
// just made.
func shaBySubject(t *testing.T, tc *TestContainer, campPath, subject string) string {
	t.Helper()
	out := tc.GitOutput(t, campPath, "log", "--format=%H %s")
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		sha, rest, ok := strings.Cut(line, " ")
		if ok && strings.HasSuffix(rest, subject) {
			return sha
		}
	}
	t.Fatalf("no commit with subject %q in:\n%s", subject, out)
	return ""
}

func TestIntegration_ManifestRecordsArtifactStatePerCommit(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := "/campaigns/manifest-record"
	_, err := tc.InitCampaign(campPath, "manifest-record", "product")
	require.NoError(t, err)
	const machine = "machine-a"

	// Declare the root, then let the declaration's own bookkeeping and any
	// first manifest settle before the commit under test.
	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		mkdir -p media
		printf 'seed-content-v1' > media/seed.bin
	`, campPath))
	campAs(t, tc, machine, campPath, "artifacts add media")
	campAs(t, tc, machine, campPath, `commit -m "declare media root"`)
	settleJobs(t, tc, machine, campPath)

	// The commit under test changes the root's contents.
	tc.Shell(t, fmt.Sprintf(`
		set -e
		cd %s
		printf 'seed-content-v2-now-longer' > media/seed.bin
		printf 'second-file-content' > media/extra.bin
		printf 'tracked change\n' >> README.md
	`, campPath))
	campAs(t, tc, machine, campPath, `commit -m "change media"`)
	head1 := shaBySubject(t, tc, campPath, "change media")

	// Criterion 21c, structurally: the commit that changed the root does not
	// itself carry the manifest. The record rides a later carrier commit;
	// nothing hashed on the command the user waited for.
	head1Files := tc.GitOutput(t, campPath, "show", "--name-only", "--format=", head1)
	assert.NotContains(t, head1Files, ".campaign/artifacts/manifests",
		"the described commit must not carry its own manifest (hashing is deferred)")

	settleJobs(t, tc, machine, campPath)

	// Criterion 16: the record exists, is committed, and ties history to
	// artifact state: path, size, mtime, hash, and the described commit.
	manifestRel := ".campaign/artifacts/manifests/" + machine + "/media.json"
	manifest := tc.Shell(t, fmt.Sprintf(`cd %s && git show HEAD:%s 2>/dev/null || cat %s`,
		campPath, manifestRel, manifestRel))
	for _, want := range []string{
		`"path": "seed.bin"`, `"path": "extra.bin"`,
		`"mtime_unix_nano"`, `"hash_sha256"`,
		fmt.Sprintf(`"describes_commit": "%s"`, head1),
	} {
		assert.Contains(t, manifest, want, "committed manifest must carry %s", want)
	}
	subjects := tc.GitOutput(t, campPath, "log", "--format=%s")
	assert.Contains(t, subjects, "manifest: media at "+head1[:8],
		"the carrier commit names the root and the described commit")

	// Criterion 17: a commit that does not touch the root re-commits nothing.
	manifestCommits := strings.Count(subjects, "manifest: media at ")
	tc.Shell(t, fmt.Sprintf(`cd %s && printf 'unrelated\n' >> README.md`, campPath))
	campAs(t, tc, machine, campPath, `commit -m "unrelated change"`)
	settleJobs(t, tc, machine, campPath)
	subjects = tc.GitOutput(t, campPath, "log", "--format=%s")
	assert.Equal(t, manifestCommits, strings.Count(subjects, "manifest: media at "),
		"an unchanged root must not produce a new manifest commit")
	manifestAfter := tc.Shell(t, fmt.Sprintf(`cd %s && git show HEAD:%s`, campPath, manifestRel))
	assert.Contains(t, manifestAfter, head1,
		"the record keeps describing the commit that last changed the root")

	// Criterion 21: camp stores no copy of artifact content anywhere.
	leaked := strings.TrimSpace(tc.Shell(t, fmt.Sprintf(
		`grep -rl 'seed-content-v2-now-longer' %s/.campaign 2>/dev/null || true`, campPath)))
	assert.Empty(t, leaked, "no camp-managed directory may hold artifact content bytes")

	// Criterion 27: drift is reported on doctor and status, and never fixed.
	tc.Shell(t, fmt.Sprintf(`cd %s && printf 'drifted-content-edit' > media/seed.bin`, campPath))
	doctorOut := campAs(t, tc, machine, campPath, "doctor -c artifacts --json")
	assert.Contains(t, doctorOut, "manifest_drift", "doctor must report the drift row")
	statusOut := campAs(t, tc, machine, campPath, "status")
	assert.Contains(t, statusOut, "drifted from its committed manifest",
		"status must surface the drift notice")
	content := tc.Shell(t, fmt.Sprintf(`cat %s/media/seed.bin`, campPath))
	assert.Equal(t, "drifted-content-edit", strings.TrimSpace(content),
		"drift is reported, never resolved: the working tree is untouched")

	// The next commit updates the record and the drift clears.
	tc.Shell(t, fmt.Sprintf(`cd %s && printf 'clear drift\n' >> README.md`, campPath))
	campAs(t, tc, machine, campPath, `commit -m "carry drifted media"`)
	settleJobs(t, tc, machine, campPath)
	doctorOut = campAs(t, tc, machine, campPath, "doctor -c artifacts --json")
	assert.NotContains(t, doctorOut, "manifest_drift",
		"a committed record matching the tree is not drift")
}

func TestIntegration_ManifestTwoMachinesNeverCollide(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := "/campaigns/manifest-machines"
	_, err := tc.InitCampaign(campPath, "manifest-machines", "product")
	require.NoError(t, err)

	tc.Shell(t, fmt.Sprintf(`cd %s && mkdir -p media && printf 'shared' > media/f.bin`, campPath))
	campAs(t, tc, "machine-a", campPath, "artifacts add media")
	campAs(t, tc, "machine-a", campPath, `commit -m "declare on a"`)
	settleJobs(t, tc, "machine-a", campPath)

	tc.Shell(t, fmt.Sprintf(`cd %s && printf 'b-view' > media/f.bin && printf 'b change\n' >> README.md`, campPath))
	campAs(t, tc, "machine-b", campPath, `commit -m "change on b"`)
	settleJobs(t, tc, "machine-b", campPath)

	// Criterion 19: distinct paths by construction; both records coexist in
	// one history with no conflict to resolve.
	tree := tc.GitOutput(t, campPath, "ls-tree", "-r", "--name-only", "HEAD")
	assert.Contains(t, tree, ".campaign/artifacts/manifests/machine-a/media.json")
	assert.Contains(t, tree, ".campaign/artifacts/manifests/machine-b/media.json")
}

func TestIntegration_DrainNeverWaitsOnAManifestHash(t *testing.T) {
	tc := GetSharedContainer(t)
	campPath := "/campaigns/manifest-drain"
	_, err := tc.InitCampaign(campPath, "manifest-drain", "product")
	require.NoError(t, err)
	const machine = "machine-a"

	tc.Shell(t, fmt.Sprintf(`cd %s && mkdir -p media && printf 'small' > media/f.bin`, campPath))
	campAs(t, tc, machine, campPath, "artifacts add media")
	campAs(t, tc, machine, campPath, `commit -m "declare"`)
	settleJobs(t, tc, machine, campPath)

	// Hold the root lane's worker lock so nothing can serve the queue: the
	// manifest job is pinned pending, and the only way the drain below can
	// return is by not waiting for it. This is the deterministic form of "a
	// multi-gigabyte first pass is still running".
	tc.Shell(t, fmt.Sprintf(
		`touch "%s/.campaign/cache/jobs/worker-%%2E.lock"`, campPath))
	tc.Shell(t, fmt.Sprintf(`cd %s && printf 'moved' > media/f.bin && printf 'busy change\n' >> README.md`, campPath))
	campAs(t, tc, machine, campPath, `commit -m "change while lane is busy"`)

	// Criterion 21b: the drain every history-moving command uses returns while
	// the manifest job cannot possibly have run. Blocking jobs only.
	campAs(t, tc, machine, campPath, "jobs drain")
	left := strings.TrimSpace(tc.Shell(t, fmt.Sprintf(
		`find %s/.campaign/cache/jobs/pending -name '*.json' 2>/dev/null | wc -l`, campPath)))
	assert.NotEqual(t, "0", left,
		"the manifest job must still be pending when the drain returns; drains wait for blocking jobs only")

	// Release the lane; the record still lands and names the right commit.
	tc.Shell(t, fmt.Sprintf(`rm -f "%s/.campaign/cache/jobs/worker-%%2E.lock"`, campPath))
	described := shaBySubject(t, tc, campPath, "change while lane is busy")
	settleJobs(t, tc, machine, campPath)
	subjects := tc.GitOutput(t, campPath, "log", "--format=%s")
	assert.Contains(t, subjects, "manifest: media at "+described[:8],
		"the manifest still lands after the drain, naming the right commit")
}
