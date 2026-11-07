package git

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSubmoduleOwnerLock_CancelWaitingForSameOwner(t *testing.T) {
	var lock SubmoduleOwnerLock
	release, err := lock.Lock(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := lock.Lock(ctx, "/")
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting lock error = %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting for a busy owner ignored cancellation")
	}
}

func TestSubmoduleOwnerLock_IndependentOwners(t *testing.T) {
	var lock SubmoduleOwnerLock
	releaseRoot, err := lock.Lock(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseRoot()
	releaseTmp, err := lock.Lock(context.Background(), "/tmp")
	if err != nil {
		t.Fatalf("different owner was blocked: %v", err)
	}
	releaseTmp()
}
