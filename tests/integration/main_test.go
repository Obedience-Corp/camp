//go:build integration
// +build integration

package integration

import (
	"os"
	"testing"
)

// sharedContainer is the package-level container reused across all tests.
// It is initialized once in TestMain and cleaned up after all tests complete.
var sharedContainer *TestContainer

// festAvailable indicates whether the fest binary was successfully built and
// copied into the shared container. Tests that require fest should skip if false.
var festAvailable bool

// TestMain sets up a shared container for all integration tests.
// This avoids the overhead of spinning up a new container for each test.
func TestMain(m *testing.M) {
	var err error
	sharedContainer, err = NewSharedContainer()
	if err != nil {
		os.Stderr.WriteString("Failed to create shared container: " + err.Error() + "\n")
		os.Exit(1)
	}

	code := m.Run()

	sharedContainer.Cleanup()
	os.Exit(code)
}

// GetSharedContainer returns the shared container, resetting state first.
// This should be called at the start of each test function.
func GetSharedContainer(t *testing.T) *TestContainer {
	t.Helper()
	if sharedContainer == nil {
		t.Fatal("shared container not initialized - TestMain not called?")
	}

	if err := sharedContainer.Reset(); err != nil {
		t.Fatalf("failed to reset container: %v", err)
	}

	// Return a new TestContainer with the current test's context
	// but sharing the same underlying container
	return &TestContainer{
		container: sharedContainer.container,
		ctx:       sharedContainer.ctx,
		t:         t,
	}
}
