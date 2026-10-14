//go:build windows

package sync

import (
	"context"
	"errors"
	"testing"

	"github.com/Obedience-Corp/camp/internal/peer"
	"github.com/Obedience-Corp/camp/internal/remote"
)

func TestWindowsPeerSyncFailsBeforePreflight(t *testing.T) {
	s := NewSyncer(`C:\missing-camp`, WithPeer(peer.FromPath("peer", `C:\missing-peer`)))
	result, err := s.Sync(context.Background())
	if result != nil || !errors.Is(err, remote.ErrWindowsPeerUnsupported) {
		t.Fatalf("peer Sync = %+v, %v; want early Windows peer gate", result, err)
	}
}
