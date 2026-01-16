package version

import (
	"runtime"
	"testing"
)

func TestGet(t *testing.T) {
	info := Get()

	// Check that basic fields are populated
	if info.Version == "" {
		t.Error("Version should not be empty")
	}

	if info.Commit == "" {
		t.Error("Commit should not be empty")
	}

	if info.BuildDate == "" {
		t.Error("BuildDate should not be empty")
	}

	// Check that runtime info is correct
	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %s; want %s", info.GoVersion, runtime.Version())
	}

	expectedPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if info.Platform != expectedPlatform {
		t.Errorf("Platform = %s; want %s", info.Platform, expectedPlatform)
	}
}

func TestDefaultValues(t *testing.T) {
	// Test that default values are set
	if Version == "" {
		t.Error("Version should have a default value")
	}

	if Commit == "" {
		t.Error("Commit should have a default value")
	}

	if BuildDate == "" {
		t.Error("BuildDate should have a default value")
	}
}
