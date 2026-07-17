package migrate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func relMoves(t *testing.T, root string, plan *Plan) []string {
	t.Helper()
	out := make([]string, 0, len(plan.Moves))
	for _, m := range plan.Moves {
		rel, err := filepath.Rel(root, m.From)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

func TestBuildPlan_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := BuildPlan(ctx, t.TempDir()); err == nil {
		t.Fatal("BuildPlan() error = nil, want a cancellation error")
	}
}

// TestBuildPlan_RefusesOnConflict is the no-partial-work guard: a parent
// holding both spellings needs a human to decide what to keep, and migrating
// the rest would leave the campaign half-converted.
func TestBuildPlan_RefusesOnConflict(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, spelling.Visible))
	mustMkdir(t, filepath.Join(root, spelling.Hidden))
	mustMkdir(t, filepath.Join(root, "workflow", "design", spelling.Visible))

	_, err := BuildPlan(context.Background(), root)
	if err == nil {
		t.Fatal("BuildPlan() error = nil, want a refusal")
	}
	if !errors.Is(err, camperrors.ErrConflict) {
		t.Errorf("errors.Is(err, ErrConflict) = false, want true (err = %v)", err)
	}
	if !strings.Contains(err.Error(), ".") {
		t.Errorf("error should name the conflicting location, got: %v", err)
	}
}

func TestBuildPlan_RefusesWhenTargetOccupiedByFile(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, spelling.Visible))
	// A file, not a directory: Discover ignores it, but it still blocks the move.
	mustWrite(t, filepath.Join(root, spelling.Hidden), "in the way")

	_, err := BuildPlan(context.Background(), root)
	if err == nil {
		t.Fatal("BuildPlan() error = nil, want a refusal: the target is occupied")
	}
	if !errors.Is(err, camperrors.ErrAlreadyExists) {
		t.Errorf("errors.Is(err, ErrAlreadyExists) = false, want true (err = %v)", err)
	}
}

// TestBuildPlan_SkipsProjects protects real repositories: projects/camp holds
// Go packages named "dungeon" that are not campaign dungeons.
func TestBuildPlan_SkipsProjects(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, spelling.Visible))
	mustMkdir(t, filepath.Join(root, spelling.ProjectsDir, "camp", "internal", "dungeon"))
	mustMkdir(t, filepath.Join(root, spelling.ProjectsDir, "camp", "cmd", "camp", "dungeon"))

	plan, err := BuildPlan(context.Background(), root)
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if got := relMoves(t, root, plan); len(got) != 1 || got[0] != spelling.Visible {
		t.Fatalf("Moves = %v, want only [%s]", got, spelling.Visible)
	}
	sep := string(filepath.Separator)
	for _, m := range plan.Moves {
		if strings.Contains(m.From, sep+spelling.ProjectsDir+sep) {
			t.Errorf("plan must never move a path under %s/: %s", spelling.ProjectsDir, m.From)
		}
	}
}

// TestBuildPlan_OrdersDeepestFirst pins the ordering the real campaign needs:
// workflow/design/dungeon/completed/<item>/dungeon lives inside another
// dungeon, so renaming the outer one first would invalidate the inner path.
func TestBuildPlan_OrdersDeepestFirst(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "workflow", "design", spelling.Visible, "completed", "go-token-counter", spelling.Visible)
	mustMkdir(t, nested)
	mustMkdir(t, filepath.Join(root, spelling.Visible))

	plan, err := BuildPlan(context.Background(), root)
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}

	got := relMoves(t, root, plan)
	want := []string{
		"workflow/design/dungeon/completed/go-token-counter/dungeon",
		"workflow/design/dungeon",
		"dungeon",
	}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("Moves = %v, want %v (deepest first)", got, want)
		}
	}
}

func TestBuildPlan_EmptyWhenAlreadyMigrated(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, spelling.Hidden))
	mustMkdir(t, filepath.Join(root, "workflow", "design", spelling.Hidden))

	plan, err := BuildPlan(context.Background(), root)
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if !plan.Empty() {
		t.Errorf("Empty() = false, want true (moves: %v)", relMoves(t, root, plan))
	}
	if len(plan.AlreadyHidden) != 2 {
		t.Errorf("AlreadyHidden = %v, want both hidden dungeons", plan.AlreadyHidden)
	}
}

func TestPlan_CommitPaths(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, spelling.Visible))
	mustMkdir(t, filepath.Join(root, "workflow", "design", spelling.Visible))

	plan, err := BuildPlan(context.Background(), root)
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	added, removed := plan.CommitPaths()
	if len(added) != 2 || len(removed) != 2 {
		t.Fatalf("CommitPaths() = %v / %v, want two of each", added, removed)
	}
	for _, p := range append(append([]string{}, added...), removed...) {
		if filepath.IsAbs(p) {
			t.Errorf("commit path %q must be repo-relative", p)
		}
	}
}

// TestPlan_CommitPathsOmitsNestedMoves pins a path that does not exist after
// Apply. A dungeon nested inside another migrated dungeon is renamed first and
// then has its ancestor renamed out from under it, so its Move.To is stale by
// the time the commit stages anything: staging it fails with "pathspec did not
// match any files". The ancestor's entry already covers the whole subtree.
func TestPlan_CommitPathsOmitsNestedMoves(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "workflow", "design", spelling.Visible, "completed", "item", spelling.Visible))
	mustMkdir(t, filepath.Join(root, spelling.Visible))

	plan, err := BuildPlan(context.Background(), root)
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if len(plan.Moves) != 3 {
		t.Fatalf("planned %d moves, want 3", len(plan.Moves))
	}

	added, removed := plan.CommitPaths()
	wantAdded := []string{"workflow/design/.dungeon", ".dungeon"}
	wantRemoved := []string{"workflow/design/dungeon", "dungeon"}
	for i, p := range added {
		added[i] = filepath.ToSlash(p)
	}
	for i, p := range removed {
		removed[i] = filepath.ToSlash(p)
	}
	if !equalStringSlices(added, wantAdded) {
		t.Errorf("added = %v, want %v", added, wantAdded)
	}
	if !equalStringSlices(removed, wantRemoved) {
		t.Errorf("removed = %v, want %v", removed, wantRemoved)
	}

	for _, p := range append(append([]string{}, added...), removed...) {
		if strings.Contains(p, "completed") {
			t.Errorf("commit path %q names a nested dungeon whose ancestor also moves; it will not exist after Apply", p)
		}
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
