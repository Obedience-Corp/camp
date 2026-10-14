//go:build windows

package clone

import (
	"context"
	"errors"
	"testing"

	"github.com/Obedience-Corp/camp/internal/peer"
	"github.com/Obedience-Corp/camp/internal/remote"
)

func TestWindowsPeerCloneFailsBeforeCreatingDestination(t *testing.T) {
	c := NewCloner(WithPeer(peer.FromPath("peer", `C:\missing-peer`)))
	result, err := c.Clone(context.Background())
	if result != nil || !errors.Is(err, remote.ErrWindowsPeerUnsupported) {
		t.Fatalf("peer Clone = %+v, %v; want early Windows peer gate", result, err)
	}
}
