//go:build integration
// +build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// These tests construct their own latches. The live run's latch (runInfra)
// must never be touched from a test: arming it would skip every checkout
// that follows in the same run.

// One faulting member is a bad container; two distinct members are a dead
// daemon. The threshold is what keeps a single wedged container from
// poisoning a healthy pool.
func TestInfraLatchArmsOnDistinctMembersOnly(t *testing.T) {
	t.Parallel()

	l := newInfraLatch(func() int { return 2 })

	if armed, msg := l.recordMember("container-a", "reset failed"); armed || msg != "" {
		t.Fatalf("one member armed the latch: armed=%v msg=%q", armed, msg)
	}
	if got := l.failure(); got != "" {
		t.Fatalf("failure() = %q before arming, want empty", got)
	}

	// The same member faulting again is still one bad member.
	if armed, _ := l.recordMember("container-a", "reset failed again"); armed {
		t.Fatal("repeat faults on one member must not arm the latch")
	}

	armed, msg := l.recordMember("container-b", "exec dropped")
	if !armed {
		t.Fatal("a second distinct member must arm the latch")
	}
	if !strings.Contains(msg, "2 distinct members faulted") {
		t.Fatalf("arming message = %q, want the distinct-member count", msg)
	}
	if !strings.Contains(msg, "INFRASTRUCTURE FAILURE (not a test failure)") {
		t.Fatalf("arming message = %q, want the infrastructure banner", msg)
	}
	if got := l.failure(); got != msg {
		t.Fatalf("failure() = %q, want the arming message", got)
	}

	// Once armed, every later report sees the same banner: one message for
	// the whole run, not one per victim.
	if armed, later := l.recordMember("container-c", "another fault"); !armed || later != msg {
		t.Fatalf("post-arming report: armed=%v msg=%q, want the original banner", armed, later)
	}
}

// Pool size 1 keeps threshold 1: a solo container has no healthy siblings to
// protect, so it fails closed.
func TestInfraLatchThresholdOneArmsImmediately(t *testing.T) {
	t.Parallel()

	l := newInfraLatch(func() int { return 1 })
	if armed, _ := l.recordMember("only-container", "gone"); !armed {
		t.Fatal("threshold 1 must arm on the first faulting member")
	}
}

// The per-exec deadline is the wedged-daemon detector: that failure mode has
// no transport signature, just an exec that never comes back.
func TestClassifyExecOutcomeDeadline(t *testing.T) {
	t.Parallel()

	cause := context.DeadlineExceeded
	got := classifyExecOutcome(cause, true)

	var ie *infraError
	if !errors.As(got, &ie) {
		t.Fatalf("a timed-out exec was not typed as infrastructure: %T %v", got, got)
	}
	if !strings.Contains(got.Error(), "did not complete within") {
		t.Fatalf("timeout classification = %q, want it to name the deadline", got)
	}
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Fatal("the label must wrap the original so the cause survives")
	}

	// Without the timeout flag, classification falls through to the
	// transport signatures: a plain error stays a plain error.
	plain := errors.New("exit status 1")
	if got := classifyExecOutcome(plain, false); got != plain {
		t.Fatalf("a real failure was relabelled: %v", got)
	}
	// And a transport fault is still labelled.
	transport := errors.New("container exec attach: unexpected EOF")
	if !errors.As(classifyExecOutcome(transport, false), &ie) {
		t.Fatal("a transport fault must classify as infrastructure")
	}

	if classifyExecOutcome(nil, false) != nil {
		t.Fatal("classifyExecOutcome(nil, false) must stay nil")
	}
}

// The telemetry line is the capacity margin on screen; its shape is pinned so
// the numbers people grep for keep their names.
func TestExecTelemetryLine(t *testing.T) {
	t.Parallel()

	if got := execTelemetryLine(nil, 4, 90*time.Second); got != "harness telemetry: execs=0 pool=4 wall=1m30s" {
		t.Fatalf("empty-run line = %q", got)
	}

	durations := []time.Duration{
		10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond,
		40 * time.Millisecond, 1 * time.Second,
	}
	got := execTelemetryLine(durations, 4, 20*time.Minute)
	for _, want := range []string{"execs=5", "p50=30ms", "p95=40ms", "max=1s", "pool=4", "wall=20m0s"} {
		if !strings.Contains(got, want) {
			t.Fatalf("telemetry line = %q, want it to contain %q", got, want)
		}
	}
}
