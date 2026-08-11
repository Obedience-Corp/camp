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
	"time"

	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
)

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
	cleanupTransport, err := startDedicatedColimaDockerTransport()
	if err != nil {
		os.Stderr.WriteString("Failed to create isolated Docker transport: " + err.Error() + "\n")
		os.Exit(1)
	}

	// Sized after the transport exists, so the daemon it asks about is the one
	// the containers will actually run on. Asking first worked by luck here
	// (both spellings reach the same daemon) but would answer for the wrong
	// machine, or not at all, against a remote or rerouted one.
	//
	// Announced because the failure this guards against is silent: an
	// oversubscribed pool looks like flaky tests, and the number that caused
	// it was never on screen.
	daemonCPUs := dockerCPUs()
	size := poolSizeFor(daemonCPUs)
	fmt.Fprintf(os.Stderr, "container pool: %d (docker daemon reports %d CPUs, host has %d)\n",
		size, daemonCPUs, runtime.NumCPU())

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

	start := time.Now()
	code := m.Run()

	// Wall is the test-execution window (pool setup and teardown excluded):
	// that is the phase whose exec rate and latency predict collapse.
	reportExecTelemetry(size, time.Since(start), code)

	for _, c := range poolMembers {
		c.Cleanup()
	}
	cleanupTransport()
	os.Exit(code)
}

// poolSize returns how many containers to run concurrently. Override with
// CAMP_TEST_POOL_SIZE; otherwise scale to the host but stay conservative, since
// each member is a full container running real git operations.
// poolSizeFor turns the Docker daemon's CPU count into a pool size. A
// non-positive count means it could not be determined.
//
// Sized against the machine that runs the containers, not the one
// orchestrating them.
//
// On a Linux host those are the same machine and nothing changes. Under
// Colima, Docker Desktop, or a remote daemon they are not, and the host is
// always the larger of the two: a 16-CPU laptop driving a 4-CPU VM sized this
// pool to six containers, half again what the VM could actually run. The
// oversubscription did not fail loudly. It showed up as tests that pass alone
// and fail in the full suite, differently each run, which reads like flaky
// tests rather than a saturated machine.
//
// The daemon's own count is used as-is, one container per CPU. The halving
// this function used to apply was calibrated for the host count, where it
// approximated a VM the host was fronting; applied to the daemon's own number
// it discounts capacity twice. On the machine above that double discount took
// the pool from six to two — a 3x wall-clock regression on a suite whose
// containers spend their time in short git execs, not sustained CPU — while
// the flakiness the sizing fix cured needed only the drop from six to four.
// The halving survives solely in the host fallback, where the count is still
// a proxy for a machine that may be smaller.
func poolSizeFor(daemonCPUs int) int {
	if v := os.Getenv("CAMP_TEST_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}

	n := daemonCPUs
	if n <= 0 {
		// Cannot tell. Fall back to the halved host count, which is what this
		// did unconditionally before: no worse than the old behavior.
		n = runtime.NumCPU() / 2
	}
	if n < 2 {
		n = 2
	}
	if n > 6 {
		n = 6
	}
	return n
}

// dockerCPUs reports the Docker daemon's CPU count, or 0 when it cannot be
// determined.
//
// Zero rather than an error: failing to size a pool optimally is not worth
// failing a test run over, and the caller falls back to the host count, which
// is what this code did unconditionally before.
func dockerCPUs() int {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return 0
	}
	defer func() { _ = provider.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	result, err := provider.Client().Info(ctx, client.InfoOptions{})
	if err != nil {
		return 0
	}
	return result.Info.NCPU
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
	if msg := runInfra.failure(); msg != "" {
		t.Skip(msg)
	}

	c := <-containerPool

	if err := c.Reset(); err != nil {
		// One retry absorbs a transient blip; a second failure means this
		// member is gone. Only arm run-level infra death after enough
		// *distinct* members fault (see infraLatch.recordMember) so one
		// wedged container does not skip the rest of a healthy pool.
		if retryErr := c.Reset(); retryErr != nil {
			containerPool <- c
			reason := fmt.Sprintf("could not reset a pooled container: %v (retry: %v)", err, retryErr)
			if armed, msg := runInfra.recordMember(c.container.GetContainerID(), reason); armed {
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
