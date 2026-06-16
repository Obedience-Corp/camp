//go:build integration
// +build integration

package integration

import (
	"context"
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

// TestMain builds a pool of identical containers once and shares them across all
// integration tests. Reusing containers avoids per-test create/destroy cost; a
// pool (rather than a single container) lets tests run concurrently via
// t.Parallel(), since each test gets exclusive use of one isolated container.
func TestMain(m *testing.M) {
	size := poolSize()

	bins, cleanupBins, err := buildSharedBinaries()
	if err != nil {
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
		os.Stderr.WriteString("Failed to create container pool: " + buildErr.Error() + "\n")
		os.Exit(1)
	}

	code := m.Run()

	for _, c := range poolMembers {
		c.Cleanup()
	}
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

	c := <-containerPool

	if err := c.Reset(); err != nil {
		// Return the container so the pool does not leak a slot, then fail.
		containerPool <- c
		t.Fatalf("failed to reset container: %v", err)
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
