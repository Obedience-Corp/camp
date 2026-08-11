//go:build integration
// +build integration

package integration

import (
	"strconv"
	"sync"
)

// containerPool holds the reusable containers. Tests check one out (blocking
// until one is free), run against it, and return it on cleanup. The buffer size
// is the maximum number of integration tests that run concurrently.
//
// Declared here rather than in main_test.go because non-test files (the exec
// funnel in helpers_container.go) participate in the infrastructure latch,
// and Go's base package cannot see symbols declared in _test.go files.
var containerPool chan *TestContainer

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
// Arming is thresholded on *distinct* pool members. A single wedged container
// must not poison healthy siblings: only when enough different members fault
// do we treat the run as infrastructure death. Pool size 1 keeps threshold 1
// so a solo container still fails closed.
//
// Members are reported from two places: a double-failed Reset at checkout
// (GetSharedContainer in main_test.go), and any infra-classified exec fault
// mid-test (execWith in helpers_container.go). The second source is what the
// 2026-08-10 collapse was missing: six mid-test transport faults rendered as
// six broken tests while the run kept dispatching into a dead daemon.

// infraLatch is the run-level infrastructure-death record. It is a type
// rather than package globals so its arming semantics are testable without
// touching the live run's state (see infra_latch_test.go).
type infraLatch struct {
	mu        sync.Mutex
	threshold func() int
	reason    string
	members   map[string]string // container ID -> first fault reason
}

func newInfraLatch(threshold func() int) *infraLatch {
	return &infraLatch{threshold: threshold, members: make(map[string]string)}
}

// recordMember notes an infrastructure fault on the pool member identified by
// container ID. Returns (armed, message): armed means the run-level infra
// banner is set and later checkouts should skip; !armed means only this
// member is bad so far and the caller should fail its own test locally
// without killing the suite. Repeat faults on one member never arm by
// themselves.
func (l *infraLatch) recordMember(id, reason string) (armed bool, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reason != "" {
		return true, l.reason
	}
	if _, seen := l.members[id]; !seen {
		l.members[id] = reason
	}
	if len(l.members) < l.threshold() {
		return false, ""
	}
	l.reason = "INFRASTRUCTURE FAILURE (not a test failure): " + reason +
		"\n\nThe container pool could not be used (" +
		strconv.Itoa(len(l.members)) + " distinct members faulted). " +
		"Common cause: the Docker daemon is out of headroom, often because " +
		"several suites or gates are running at once. Re-run the suite on an " +
		"idle machine, or lower CAMP_TEST_POOL_SIZE."
	return true, l.reason
}

// failure returns the recorded run-level fault, or "" when the run is still
// healthy (including when only a single pool member has faulted).
func (l *infraLatch) failure() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reason
}

// runInfra is the latch for this run. Exec faults and Reset double-failures
// both report here.
var runInfra = newInfraLatch(infraMemberThreshold)

// infraMemberThreshold is how many distinct pool members must fault before
// the run is declared infrastructure-dead.
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
