//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
)

// containerPool holds the reusable containers. Tests check one out (blocking
// until one is free), run against it, and return it on cleanup. The buffer size
// is the maximum number of integration tests that run concurrently.
var containerPool chan *TestContainer

// poolMembers retains every container created so TestMain can tear them all down
// after the run, independent of what is currently parked in the pool channel.
var poolMembers []*TestContainer

// festAvailable indicates whether the fest binary was successfully built and
// copied into the pooled containers. Tests that require fest should skip if false.
var festAvailable bool

// sccAvailable indicates whether the scc binary was successfully built and
// copied into the pooled containers. Leverage tests that require scc should
// skip if false.
var sccAvailable bool

// legacyCampSkip is set to a non-empty reason when the pinned pre-reader camp
// binary (/camp-legacy) could not be built (e.g. its commit is absent in a
// shallow clone). The criterion-17 rollout-contract test skips with this reason.
var legacyCampSkip string

// TestMain builds a pool of identical containers once and shares them across all
// integration tests. Reusing containers avoids per-test create/destroy cost; a
// pool (rather than a single container) lets tests run concurrently via
// t.Parallel(), since each test gets exclusive use of one isolated container.
func TestMain(m *testing.M) {
	size := poolSize()

	cleanupTransport, err := startDedicatedColimaDockerTransport()
	if err != nil {
		os.Stderr.WriteString("Failed to create isolated Docker transport: " + err.Error() + "\n")
		os.Exit(1)
	}

	bins, cleanupBins, err := buildSharedBinaries()
	if err != nil {
		cleanupTransport()
		os.Stderr.WriteString("Failed to build test binaries: " + err.Error() + "\n")
		os.Exit(1)
	}

	containerPool = make(chan *TestContainer, size)
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		buildErr error
	)
	for range size {
		wg.Go(func() {
			c, err := newPooledContainer(context.Background(), bins)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if buildErr == nil {
					buildErr = err
				}
				return
			}
			poolMembers = append(poolMembers, c)
			containerPool <- c
		})
	}
	wg.Wait()

	// Binaries are now copied into every container; the host temp dirs can go.
	cleanupBins()

	if buildErr != nil {
		for _, c := range poolMembers {
			c.Cleanup()
		}
		cleanupTransport()
		os.Stderr.WriteString("Failed to create container pool: " + buildErr.Error() + "\n")
		os.Exit(1)
	}

	code := m.Run()

	for _, c := range poolMembers {
		c.Cleanup()
	}
	cleanupTransport()
	os.Exit(code)
}

// poolSize returns how many containers to run concurrently. Override with
// CAMP_TEST_POOL_SIZE; otherwise scale to the host but stay conservative, since
// each member is a full container running real git operations.
func poolSize() int {
	if v := os.Getenv("CAMP_TEST_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	n := runtime.NumCPU() / 2
	if n < 2 {
		n = 2
	}
	if n > 6 {
		n = 6
	}
	return n
}

// Infrastructure failure is tracked separately from test failure.
//
// When the Docker daemon runs out of headroom, every pooled checkout fails the
// same way, and each one surfaces as an ordinary test failure. A real run
// produced 474 of 572 tests "failing" for this reason, on a branch where
// nothing was wrong: the failing run gave up after 142s where a passing run of
// the same suite took 471s. That signal actively misleads, because the natural
// reading of 474 failures is that the branch broke something fundamental, and
// ruling that out costs a full re-run plus separate investigation.
//
// The fix is not to make acquisition more reliable, which is not in this
// harness's gift, but to make its failure legible and to stop early: one
// unmistakable message beats hundreds of plausible ones.
//
// Arming is thresholded on *distinct* pool members (not one double-Reset). A
// single wedged container must not poison healthy siblings: only when enough
// different members fail do we treat the run as infrastructure death. Pool
// size 1 keeps threshold 1 so a solo container still fails closed.
var (
	infraMu            sync.Mutex
	infraReason        string
	infraFailedMembers map[*TestContainer]string // first double-Reset reason per member
)

// infraMemberThreshold is how many distinct pool members must double-fail
// Reset before the run is declared infrastructure-dead.
func infraMemberThreshold() int {
	if containerPool == nil {
		return 1
	}
	// cap(pool) is the fixed pool size set in TestMain.
	if n := cap(containerPool); n >= 2 {
		return 2
	}
	return 1
}

// recordMemberResetFailure notes a double-Reset failure on member c. Returns
// (armed, message): armed means the run-level infra banner is set and later
// checkouts should skip; !armed means only this member is bad so far and the
// caller should fail this test locally without killing the suite.
func recordMemberResetFailure(c *TestContainer, reason string) (armed bool, message string) {
	infraMu.Lock()
	defer infraMu.Unlock()
	if infraReason != "" {
		return true, infraReason
	}
	if infraFailedMembers == nil {
		infraFailedMembers = make(map[*TestContainer]string)
	}
	if _, seen := infraFailedMembers[c]; !seen {
		infraFailedMembers[c] = reason
	}
	if len(infraFailedMembers) < infraMemberThreshold() {
		return false, ""
	}
	infraReason = "INFRASTRUCTURE FAILURE (not a test failure): " + reason +
		"\n\nThe container pool could not be used (" +
		strconv.Itoa(len(infraFailedMembers)) + " distinct members failed Reset). " +
		"Common cause: the Docker daemon is out of headroom, often because " +
		"several suites or gates are running at once. Re-run the suite on an " +
		"idle machine, or lower CAMP_TEST_POOL_SIZE."
	return true, infraReason
}

// infraFailure returns the recorded run-level fault, or "" when the run is
// still healthy (including when only a single pool member has failed).
func infraFailure() string {
	infraMu.Lock()
	defer infraMu.Unlock()
	return infraReason
}

// GetSharedContainer checks a container out of the pool for the calling test,
// marks the test parallel, and resets the container to a clean state. The
// container is returned to the pool when the test and all its subtests finish.
// Each checkout resets before use, so
// tests can execute concurrently despite sharing hardcoded paths like /test.
func GetSharedContainer(t *testing.T) *TestContainer {
	t.Helper()
	t.Parallel()

	if containerPool == nil {
		t.Fatal("container pool not initialized - TestMain not called?")
	}

	// A run whose infrastructure has already collapsed should say so once and
	// stop, rather than reporting the same fault as N test failures. See
	// failInfrastructure for why this distinction matters.
	//
	// The first double-Reset failure uses t.Fatalf (the real infra death).
	// Later checkouts t.Skip so the summary is 1 fail + N skips, not hundreds
	// of identical hard fails with a contradictory "Skipped:" banner.
	if msg := infraFailure(); msg != "" {
		t.Skip(msg)
	}

	c := <-containerPool

	if err := c.Reset(); err != nil {
		// One retry absorbs a transient blip; a second failure means this
		// member is gone. Only arm run-level infra death after enough
		// *distinct* members fail (see recordMemberResetFailure) so one
		// wedged container does not skip the rest of a healthy pool.
		if retryErr := c.Reset(); retryErr != nil {
			containerPool <- c
			reason := fmt.Sprintf("could not reset a pooled container: %v (retry: %v)", err, retryErr)
			if armed, msg := recordMemberResetFailure(c, reason); armed {
				t.Fatalf("%s", msg)
			}
			t.Fatalf("could not reset one pooled container (member-local; "+
				"run continues on other pool members until %d distinct members fail): %v (retry: %v)",
				infraMemberThreshold(), err, retryErr)
		}
	}

	// Register cleanup only after a successful checkout so the container is
	// returned to the pool exactly once.
	t.Cleanup(func() {
		containerPool <- c
	})

	// Return a wrapper bound to this test's context sharing the checked-out
	// underlying container.
	return &TestContainer{
		container: c.container,
		ctx:       c.ctx,
		t:         t,
	}
}
