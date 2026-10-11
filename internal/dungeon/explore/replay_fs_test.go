//go:build container_fs

package explore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeReplayOversizeFileKeepsPoster(t *testing.T) {
	path := filepath.Join(t.TempDir(), "festival-replay.gif")
	data := padPastSizeBudget(t, recorderReplay(t, 64, 8))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeReplay(context.Background(), path, 64, 64)
	if !errors.Is(err, ErrReplayTooLong) {
		t.Fatalf("DecodeReplay() error = %v, want ErrReplayTooLong", err)
	}
	if got.Frames != nil {
		t.Fatalf("kept %d frames for a file past the size budget", len(got.Frames))
	}
	if got.Poster == nil || posterColours(got.Poster) < 2 {
		t.Fatalf("a %d byte replay returned no composited poster", len(data))
	}
}
