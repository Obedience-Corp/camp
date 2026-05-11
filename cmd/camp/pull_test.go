package main

import (
	"context"
	"errors"
	"testing"
)

func TestRunGitPullWithLockRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	output, err := runGitPullWithLockRetry(ctx, "/path/that/should/not/be/touched", nil, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runGitPullWithLockRetry() error = %v, want context.Canceled", err)
	}
	if len(output) != 0 {
		t.Fatalf("runGitPullWithLockRetry() output = %q, want empty output", output)
	}
}
