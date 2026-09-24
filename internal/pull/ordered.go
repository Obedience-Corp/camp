package pull

import (
	"context"
	"sync"
)

// DefaultParallel is how many submodules pull at once when Options.Parallel is unset.
// Each pull locks only its own repository, so this is bounded by the remote round
// trip rather than by superproject lock contention the way sync and clone are.
const DefaultParallel = 8

// runOrdered starts work for indexes 0..n-1 in order with at most parallel in flight, and calls
// emit for each index in index order as soon as that index and all before it finish.
// Work that finished is always emitted; after cancellation, emission stops at the
// first index that never started and the context error is returned.
// emit always runs on the calling goroutine, so it needs no synchronization.
func runOrdered(ctx context.Context, n, parallel int, work, emit func(int)) error {
	if parallel < 1 {
		parallel = DefaultParallel
	}

	done := make([]chan struct{}, n)
	ran := make([]bool, n)
	for i := range done {
		done[i] = make(chan struct{})
	}

	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	defer wg.Wait()

	wg.Go(func() {
		for i := range n {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
			}
			if ctx.Err() != nil {
				for _, ch := range done[i:] {
					close(ch)
				}
				return
			}
			wg.Go(func() {
				defer close(done[i])
				defer func() { <-sem }()
				work(i)
				ran[i] = true
			})
		}
	})

	for i := range n {
		<-done[i]
		if !ran[i] {
			return ctx.Err()
		}
		emit(i)
	}
	return nil
}
