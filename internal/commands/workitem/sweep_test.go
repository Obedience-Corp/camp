package workitem

import (
	"os"
	"path/filepath"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

// TestResolveSweepLocation_WorkflowHome locks today's pass-through behavior:
// a design item resolves to its type-local dungeon at workflow/design/dungeon,
// outside a dungeon. When phase 3 adds festivals/ support to DetectFromCwd this
// baseline stays valid and the festivals/ negative case below flips; that is
// the visible seam this task exists to create.
func TestResolveSweepLocation_WorkflowHome(t *testing.T) {
	root := t.TempDir()
	itemRel := filepath.Join("workflow", "design", "some-item")
	if err := os.MkdirAll(filepath.Join(root, itemRel), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	loc, err := resolveSweepLocation(root, wkitem.WorkItem{RelativePath: itemRel})
	if err != nil {
		t.Fatalf("resolveSweepLocation: %v", err)
	}
	if loc.InDungeon {
		t.Errorf("InDungeon = true, want false for a live workflow home")
	}
	wantDungeon := filepath.Join(root, "workflow", "design", "dungeon")
	// EvalSymlinks resolves the temp root (macOS /var -> /private/var), so
	// compare on the resolved root rather than the raw t.TempDir() value.
	if resolved, rerr := filepath.EvalSymlinks(root); rerr == nil {
		wantDungeon = filepath.Join(resolved, "workflow", "design", "dungeon")
	}
	if loc.DungeonPath != wantDungeon {
		t.Errorf("DungeonPath = %q, want %q", loc.DungeonPath, wantDungeon)
	}
}

// TestResolveSweepLocation_FestivalsHomeNotYetSupported locks the current
// error for a festivals/ path. No sweep candidate has such a RelativePath
// before phase 3 (PlanSweep excludes festivals), so this asserts the baseline
// that phase 3 must deliberately change when it teaches DetectFromCwd about
// festivals/ homes. Asserting the exact message catches a silent behavior drift.
func TestResolveSweepLocation_FestivalsHomeNotYetSupported(t *testing.T) {
	root := t.TempDir()
	itemRel := filepath.Join("festivals", "ready", "foo-item")
	if err := os.MkdirAll(filepath.Join(root, itemRel), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	_, err := resolveSweepLocation(root, wkitem.WorkItem{RelativePath: itemRel})
	if err == nil {
		t.Fatal("expected error for festivals/ home, got nil")
	}
	const want = "not inside a workitem; cwd must be under workflow/<type>/<slug>/"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}
