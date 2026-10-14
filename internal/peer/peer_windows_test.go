//go:build windows

package peer

import (
	"context"
	"errors"
	"testing"

	"github.com/Obedience-Corp/camp/internal/remote"
)

func TestWindowsPeerResolutionFailsBeforeMachineLookup(t *testing.T) {
	_, err := FromMachine(context.Background(), "not-configured", "camp")
	if !errors.Is(err, remote.ErrWindowsPeerUnsupported) || !errors.Is(err, ErrPeerConfig) {
		t.Fatalf("Windows peer resolution error = %v; want support and hard-fail classes", err)
	}
}
