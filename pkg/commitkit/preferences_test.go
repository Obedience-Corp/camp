package commitkit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Obedience-Corp/camp/pkg/commitkit"
)

func TestCommitTagsEnabledCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	enabled, err := commitkit.CommitTagsEnabled(ctx, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if enabled {
		t.Fatal("a failed preference lookup must not enable tags")
	}
}
