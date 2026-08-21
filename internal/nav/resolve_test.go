package nav

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRelativePathNavigation_NestedFestival(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "festivals", "planning", "weekly-streaks")
	if err := os.MkdirAll(want, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "festivals", "planning", "push-reminders"), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveRelativePathNavigation(context.Background(), root, "festivals/", "weekly")
	if err != nil {
		t.Fatalf("ResolveRelativePathNavigation() error = %v", err)
	}
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestResolveRelativePathNavigation_StatusBucketStillWins(t *testing.T) {
	root := t.TempDir()
	planning := filepath.Join(root, "festivals", "planning")
	if err := os.MkdirAll(filepath.Join(planning, "weekly-streaks"), 0755); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveRelativePathNavigation(context.Background(), root, "festivals/", "planning")
	if err != nil {
		t.Fatalf("ResolveRelativePathNavigation() error = %v", err)
	}
	if got != planning {
		t.Fatalf("path = %q, want %q", got, planning)
	}
}

func TestResolveRelativePathNavigation_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ResolveRelativePathNavigation(ctx, t.TempDir(), "festivals/", "weekly")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}
