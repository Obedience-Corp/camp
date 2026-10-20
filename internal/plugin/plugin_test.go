package plugin

import (
	"context"
	"testing"
)

func TestLookup_NotFound(t *testing.T) {
	_, found := Lookup("nonexistent-plugin-xyz-123")
	if found {
		t.Error("expected Lookup to return false for nonexistent plugin")
	}
}

func TestDiscover_EmptyPath(t *testing.T) {
	t.Setenv("PATH", "")

	plugins, err := Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("got %d plugins, want 0", len(plugins))
	}
}

func TestDiscover_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Discover(ctx)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestExecute_CancelledContextBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := Plugin{Name: "test", Path: "/nonexistent-camp-plugin-binary"}
	err := Execute(ctx, p, nil, "")
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}
