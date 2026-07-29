package commit

import (
	"os"
	"testing"

	"github.com/Obedience-Corp/camp/internal/defercommit"
)

// Every test in this package asserts the synchronous commit contract: what gets
// staged, what the scope expands to, what the real index looks like afterward.
// Deferral changes when the commit happens, not any of that, so these tests pin
// it off rather than being rewritten to drain a queue they are not about.
//
// This is what CAMP_NO_DEFER exists for. A harness that needs strict
// determinism sets it and gets exactly the behavior camp had before deferral,
// and using it here is the same escape hatch, not a test-only special case.
//
// The deferred path has its own coverage in tests/integration, where a real
// worker and a real repository can show that the eventual commit is the same
// one this package asserts synchronously.
func TestMain(m *testing.M) {
	if err := os.Setenv(defercommit.EnvNoDefer, "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
