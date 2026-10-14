//go:build windows

package remote

import (
	"context"
	"errors"
	"testing"
)

func TestWindowsSSHPeerFailsBeforeDial(t *testing.T) {
	if _, err := Run(context.Background(), "not-a-real-peer.invalid", nil, "echo copied"); !errors.Is(err, ErrWindowsPeerUnsupported) {
		t.Fatalf("remote Run error = %v; want Windows peer gate", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, "not-a-real-peer.invalid", nil, "echo copied"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled remote Run error = %v; want context cancellation", err)
	}
}
