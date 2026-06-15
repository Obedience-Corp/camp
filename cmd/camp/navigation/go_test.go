package navigation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/state"
	"github.com/spf13/cobra"
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

func TestRunGo_ListPrintMutuallyExclusive(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("print", true, "")
	cmd.Flags().StringArrayP("command", "c", nil, "")
	cmd.Flags().Bool("root", false, "")
	cmd.Flags().BoolP("list", "l", true, "")

	err := runGo(cmd, nil)
	if err == nil {
		t.Fatal("expected --list/--print conflict error")
	}
	if !strings.Contains(err.Error(), "--list and --print are mutually exclusive") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureResolvedPrintPath_RebuildsStaleCache(t *testing.T) {
	ctx := context.Background()
	root := testNavRoot(t)
	targetPath := filepath.Join(root, "projects", "app")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}

	stalePath := filepath.Join(root, "projects", "deleted-app")
	result := &index.ResolveResult{Path: stalePath, Name: "app", Category: nav.CategoryProjects}
	opts := index.ResolveOptions{CampaignRoot: root, Category: nav.CategoryProjects, Query: "app"}

	refreshed, err := ensureResolvedPrintPath(ctx, root, opts, result)
	if err != nil {
		t.Fatalf("ensureResolvedPrintPath() error = %v", err)
	}
	if refreshed.Path != targetPath {
		t.Fatalf("refreshed path = %q, want %q", refreshed.Path, targetPath)
	}
}

func TestEnsureResolvedPrintPath_DeletedTargetReturnsClearError(t *testing.T) {
	ctx := context.Background()
	root := testNavRoot(t)
	stalePath := filepath.Join(root, "projects", "deleted-app")
	stale := index.NewIndex(root)
	stale.AddTarget(index.Target{Name: "deleted-app", Path: stalePath, Category: nav.CategoryProjects})
	if err := index.Save(stale, root); err != nil {
		t.Fatal(err)
	}

	result := &index.ResolveResult{Path: stalePath, Name: "deleted-app", Category: nav.CategoryProjects}
	opts := index.ResolveOptions{CampaignRoot: root, Category: nav.CategoryProjects, Query: "deleted-app"}

	_, err := ensureResolvedPrintPath(ctx, root, opts, result)
	if err == nil {
		t.Fatal("expected missing resolved path error")
	}
	if !strings.Contains(err.Error(), "resolved path does not exist: "+stalePath) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testNavRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}
