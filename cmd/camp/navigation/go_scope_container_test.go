//go:build container_fs

package navigation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/state"
)

func TestHandleToggleIgnoresOutOfRootHistory(t *testing.T) {
	ctx := t.Context()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "camp-a")
	other := filepath.Join(base, "camp-b")
	app := filepath.Join(root, "projects", "app")
	for _, d := range []string{app, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.SetLastLocation(ctx, root, app); err != nil {
		t.Fatal(err)
	}
	polluted := `{"location":"` + other + `","rel":"../camp-b","ts":"2099-01-01T00:00:00Z","kind":"toggle"}` + "\n"
	f, err := os.OpenFile(state.StatePath(root), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(polluted); err != nil {
		t.Fatal(err)
	}
	f.Close()

	t.Chdir(root)
	if err := handleToggle(ctx, root, true); err != nil {
		t.Fatalf("handleToggle() = %v", err)
	}
	last, err := state.GetLastLocation(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if last != root {
		t.Fatalf("last location = %q, want the camp root %q (toggle must have gone to %q, not %q)", last, root, app, other)
	}
}

func TestHandleToggleFromOutsideRootRecordsNothing(t *testing.T) {
	ctx := t.Context()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "camp-a")
	other := filepath.Join(base, "camp-b")
	app := filepath.Join(root, "projects", "app")
	for _, d := range []string{app, other} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.SetLastLocation(ctx, root, app); err != nil {
		t.Fatal(err)
	}

	t.Chdir(other)
	recordNavJump(ctx, root, root)

	last, err := state.GetLastLocation(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if last != app {
		t.Fatalf("last location = %q, want %q", last, app)
	}
}
