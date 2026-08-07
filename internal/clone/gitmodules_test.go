package clone

import (
	"context"
	"testing"
)

func TestParseGitmodules_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := parseGitmodules(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("parseGitmodules() error = %v, want context.Canceled", err)
	}
}
