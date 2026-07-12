package bginit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoHeavyCharmDeps guards the invariant that makes the startup seed work:
// internal/bginit must initialize before the bubbletea subtree, so its import
// closure may never pull in bubbletea, huh, or glamour, directly or
// transitively. If one of those crept in, its package init could run first and
// re-issue the OSC 11 / DSR terminal query this package exists to prevent.
func TestNoHeavyCharmDeps(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain unavailable")
	}

	cmd := exec.Command("go", "list", "-deps", "./internal/bginit")
	cmd.Dir = moduleRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps ./internal/bginit: %v", err)
	}

	forbidden := []string{
		"github.com/charmbracelet/bubbletea",
		"github.com/charmbracelet/huh",
		"github.com/charmbracelet/glamour",
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg := strings.TrimSpace(line)
		for _, bad := range forbidden {
			if pkg == bad || strings.HasPrefix(pkg, bad+"/") {
				t.Errorf("internal/bginit must not depend on %s (found %s); "+
					"it must initialize before the bubbletea subtree", bad, pkg)
			}
		}
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
