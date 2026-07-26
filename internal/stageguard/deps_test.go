package stageguard

import (
	"os/exec"
	"strings"
	"testing"
)

// gitPackage is the import path this package must never reach. internal/git
// imports stageguard, so an edge back would be an import cycle; the compiler
// would catch it, but only after the wiring in internal/git lands. This test
// catches it at the moment it is introduced instead.
const gitPackage = "github.com/Obedience-Corp/camp/internal/git"

func TestStageguardDoesNotDependOnInternalGit(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}

	out, err := exec.Command(goBin, "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps . error = %v", err)
	}

	for dep := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(dep) == gitPackage {
			t.Fatalf("internal/stageguard depends on %s; it is a leaf package and must not, "+
				"or the import cycle with internal/git returns", gitPackage)
		}
	}
}

// The leaf position also bounds which camp packages stageguard may import
// directly, so the dependency does not creep back in through a new edge.
func TestStageguardDirectCampImports(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}

	out, err := exec.Command(goBin, "list", "-f", `{{join .Imports "\n"}}`, ".").Output()
	if err != nil {
		t.Fatalf("go list -f imports . error = %v", err)
	}

	allowed := map[string]bool{
		"github.com/Obedience-Corp/camp/internal/config": true,
		"github.com/Obedience-Corp/camp/internal/errors": true,
	}
	for raw := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		imp := strings.TrimSpace(raw)
		if !strings.HasPrefix(imp, "github.com/Obedience-Corp/camp/") {
			continue
		}
		if !allowed[imp] {
			t.Errorf("unexpected camp import %q; stageguard may import only internal/config and internal/errors", imp)
		}
	}
}
