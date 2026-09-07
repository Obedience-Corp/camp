package postmutation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestContext_SurvivesTheCallersCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	detached, release := Context(parent)
	defer release()

	cancel()
	<-parent.Done()

	if err := detached.Err(); err != nil {
		t.Fatalf("detached context reports %v; post-mutation work must outlive the caller", err)
	}
	if !errors.Is(parent.Err(), context.Canceled) {
		t.Fatalf("parent err = %v, want context.Canceled", parent.Err())
	}
}

func TestContext_IsBounded(t *testing.T) {
	detached, release := Context(context.Background())
	defer release()

	deadline, ok := detached.Deadline()
	if !ok {
		t.Fatal("detached context has no deadline; an unbounded one makes every downstream ctx.Err() check dead code")
	}
	if remaining := time.Until(deadline); remaining > Budget || remaining <= 0 {
		t.Fatalf("deadline is %v away, want a positive value no greater than %v", remaining, Budget)
	}
}

func TestContext_ReleaseStopsIt(t *testing.T) {
	detached, release := Context(context.Background())
	release()

	if err := detached.Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("after release err = %v, want context.Canceled so the caller can stop the work it started", err)
	}
}

func TestContext_KeepsValues(t *testing.T) {
	type key struct{}
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "carried"))
	defer cancel()
	detached, release := Context(parent)
	defer release()

	if got := detached.Value(key{}); got != "carried" {
		t.Fatalf("detached context dropped the caller's values (got %v); only cancellation is detached", got)
	}
}
