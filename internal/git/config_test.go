package git

import (
	"context"
	"testing"
)

func TestGetUserName(t *testing.T) {
	ctx := context.Background()

	// This test assumes git is configured on the system.
	// If not configured, it should return empty string (not error).
	name := GetUserName(ctx)

	// We can't assert a specific value since it depends on the environment,
	// but we can verify the function doesn't panic and returns a string.
	t.Logf("GetUserName returned: %q", name)
}

func TestGetUserName_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should return empty string when context is cancelled
	name := GetUserName(ctx)
	if name != "" {
		t.Errorf("expected empty string with cancelled context, got %q", name)
	}
}
