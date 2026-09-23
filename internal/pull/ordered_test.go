package pull

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunOrderedEmitsInIndexOrderWhenLaterWorkFinishesFirst(t *testing.T) {
	const n = 6
	release := make([]chan struct{}, n)
	for i := range release {
		release[i] = make(chan struct{})
	}
	var finished sync.WaitGroup
	finished.Add(n - 1)
	go func() {
		for i := n - 1; i >= 1; i-- {
			close(release[i])
		}
		finished.Wait()
		close(release[0])
	}()

	var emitted []int
	err := runOrdered(context.Background(), n, n,
		func(i int) {
			<-release[i]
			if i != 0 {
				finished.Done()
			}
		},
		func(i int) { emitted = append(emitted, i) },
	)
	if err != nil {
		t.Fatalf("runOrdered() error = %v", err)
	}
	if want := []int{0, 1, 2, 3, 4, 5}; !slices.Equal(emitted, want) {
		t.Fatalf("emitted = %v, want %v", emitted, want)
	}
}

func TestRunOrderedBoundsConcurrency(t *testing.T) {
	const n, parallel = 20, 3
	var inFlight, peak atomic.Int32

	err := runOrdered(context.Background(), n, parallel,
		func(int) {
			cur := inFlight.Add(1)
			for {
				old := peak.Load()
				if cur <= old || peak.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			inFlight.Add(-1)
		},
		func(int) {},
	)
	if err != nil {
		t.Fatalf("runOrdered() error = %v", err)
	}
	if got := peak.Load(); got != parallel {
		t.Fatalf("peak concurrency = %d, want %d", got, parallel)
	}
}

func TestRunOrderedStopsOnCancel(t *testing.T) {
	const n = 10
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var started atomic.Int32
	var emitted []int
	err := runOrdered(ctx, n, 1,
		func(i int) {
			started.Add(1)
			if i == 1 {
				cancel()
			}
		},
		func(i int) { emitted = append(emitted, i) },
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runOrdered() error = %v, want context.Canceled", err)
	}
	if !slices.Equal(emitted, []int{0, 1}) {
		t.Fatalf("emitted = %v, want [0 1]: finished work is reported, unstarted work is not", emitted)
	}
	if got := started.Load(); got != 2 {
		t.Fatalf("started = %d, want 2: queued work must not start after cancel", got)
	}
}

func TestRunOrderedDefaultsParallelWhenUnset(t *testing.T) {
	var calls atomic.Int32
	err := runOrdered(context.Background(), 4, 0,
		func(int) { calls.Add(1) },
		func(int) {},
	)
	if err != nil {
		t.Fatalf("runOrdered() error = %v", err)
	}
	if got := calls.Load(); got != 4 {
		t.Fatalf("work calls = %d, want 4", got)
	}
}
