package navigation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/state"
)

func TestHandleToggle_ReturnsLastLocation(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()

	// Create a target directory and save it as last location
	target := filepath.Join(campaignRoot, "projects", "foo")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := state.SetLastLocation(ctx, campaignRoot, target); err != nil {
		t.Fatal(err)
	}

	// CWD must differ from target for toggle to work
	cwd, _ := os.Getwd()
	cwdReal, _ := evalSymlinks(cwd)
	targetReal, _ := evalSymlinks(target)
	if cwdReal == targetReal {
		t.Skip("cwd equals target, cannot test toggle")
	}

	err := handleToggle(ctx, campaignRoot, true)
	if err != nil {
		t.Fatalf("handleToggle() error: %v", err)
	}

	// After toggle, last location should be updated to our CWD (bounce-back)
	newLast, err := state.GetLastLocation(ctx, campaignRoot)
	if err != nil {
		t.Fatalf("GetLastLocation() error: %v", err)
	}
	newLastReal, _ := evalSymlinks(newLast)
	if newLastReal != cwdReal {
		t.Errorf("bounce-back: last location = %q, want %q", newLastReal, cwdReal)
	}
}

func TestHandleToggle_NoHistory(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()

	// No state file — toggle should error
	err := handleToggle(ctx, campaignRoot, true)
	if err == nil {
		t.Fatal("expected error for empty history")
	}
	if err.Error() != "no previous location in history" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHandleToggle_AlreadyAtLastLocation(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()

	// Save CWD as the last location
	cwd, _ := os.Getwd()
	if err := state.SetLastLocation(ctx, campaignRoot, cwd); err != nil {
		t.Fatal(err)
	}

	err := handleToggle(ctx, campaignRoot, true)
	if err == nil {
		t.Fatal("expected error when already at last location")
	}
	if err.Error() != "already at last visited location" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHandleToggle_BounceBack(t *testing.T) {
	ctx := context.Background()
	campaignRoot := t.TempDir()

	// Create two directories
	dirA := filepath.Join(campaignRoot, "a")
	dirB := filepath.Join(campaignRoot, "b")
	if err := os.MkdirAll(dirA, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirB, 0755); err != nil {
		t.Fatal(err)
	}

	// Save dirB as last location
	if err := state.SetLastLocation(ctx, campaignRoot, dirB); err != nil {
		t.Fatal(err)
	}

	// Simulate being in dirA
	origDir, _ := os.Getwd()
	if err := os.Chdir(dirA); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// First toggle: should jump to dirB, save dirA
	err := handleToggle(ctx, campaignRoot, true)
	if err != nil {
		t.Fatalf("first toggle error: %v", err)
	}

	lastLoc, _ := state.GetLastLocation(ctx, campaignRoot)
	lastReal, _ := evalSymlinks(lastLoc)
	dirAReal, _ := evalSymlinks(dirA)
	if lastReal != dirAReal {
		t.Errorf("after first toggle: last = %q, want %q", lastReal, dirAReal)
	}

	// Now chdir to dirB (simulating the shell cd)
	if err := os.Chdir(dirB); err != nil {
		t.Fatal(err)
	}

	// Second toggle: should jump back to dirA, save dirB
	err = handleToggle(ctx, campaignRoot, true)
	if err != nil {
		t.Fatalf("second toggle error: %v", err)
	}

	lastLoc, _ = state.GetLastLocation(ctx, campaignRoot)
	lastReal, _ = evalSymlinks(lastLoc)
	dirBReal, _ := evalSymlinks(dirB)
	if lastReal != dirBReal {
		t.Errorf("after second toggle: last = %q, want %q", lastReal, dirBReal)
	}
}

func TestFormatConfigShortcuts_ShowsCanonicalIntentPath(t *testing.T) {
	output := formatConfigShortcuts(map[string]config.ShortcutConfig{
		"i": {
			Path: ".campaign/intents/",
		},
	})

	if !strings.Contains(output, "i") {
		t.Fatalf("formatConfigShortcuts() missing intent shortcut: %q", output)
	}
	if !strings.Contains(output, ".campaign/intents") {
		t.Fatalf("formatConfigShortcuts() missing canonical intent path: %q", output)
	}
	if strings.Contains(output, "workflow/intents") {
		t.Fatalf("formatConfigShortcuts() should not mention legacy intent path: %q", output)
	}
}
