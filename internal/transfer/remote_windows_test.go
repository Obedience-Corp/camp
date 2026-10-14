//go:build windows

package transfer

import (
	"context"
	"errors"
	"testing"

	"github.com/Obedience-Corp/camp/internal/remote"
)

func TestWindowsRemoteCopyFailsBeforeAnyTransfer(t *testing.T) {
	run := false
	lookup := false
	err := CopyRemote(context.Background(), CopyOptions{
		Machine: testMachine(),
		Run: func(context.Context, string, ...string) ([]byte, error) {
			run = true
			return []byte("copied"), nil
		},
		LookPath: func(string) (string, error) {
			lookup = true
			return "rsync", nil
		},
	})
	if !errors.Is(err, remote.ErrWindowsPeerUnsupported) || run || lookup {
		t.Fatalf("Windows copy error = %v, run=%v lookup=%v; want no transfer attempt", err, run, lookup)
	}
}
