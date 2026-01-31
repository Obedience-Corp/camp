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

func TestGetUserEmail(t *testing.T) {
	ctx := context.Background()

	email := GetUserEmail(ctx)
	t.Logf("GetUserEmail returned: %q", email)
}

func TestGetUserIdentity(t *testing.T) {
	ctx := context.Background()

	identity := GetUserIdentity(ctx)
	t.Logf("GetUserIdentity returned: %q", identity)

	// If we have a name, the identity should not be empty
	name := GetUserName(ctx)
	if name != "" && identity == "" {
		t.Error("expected non-empty identity when name is configured")
	}
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
