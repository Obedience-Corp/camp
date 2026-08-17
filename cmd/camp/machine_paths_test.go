package main

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/machines"
)

func mustMachinesPath(t *testing.T) string {
	t.Helper()
	path, err := machines.MachinesPath()
	if err != nil {
		t.Fatalf("machines.MachinesPath() error = %v", err)
	}
	return path
}

func mustDeclinedPath(t *testing.T) string {
	t.Helper()
	path, err := machines.DeclinedPath()
	if err != nil {
		t.Fatalf("machines.DeclinedPath() error = %v", err)
	}
	return path
}

func mustMachineCacheDir(t *testing.T) string {
	t.Helper()
	dir, ok := machineCacheDir()
	if !ok {
		t.Fatal("machineCacheDir() reported no cache location")
	}
	return dir
}

func mustVersionCachePath(t *testing.T, id string) string {
	t.Helper()
	path, ok := versionCachePath(id)
	if !ok {
		t.Fatalf("versionCachePath(%q) reported no cache location", id)
	}
	return path
}

// The per-machine caches are derived data on the completion keystroke path, so
// an unresolvable home is a miss and a silent no-op, never an error and never a
// stray .obey/ directory under the operator's working directory.
func TestMachineCachesDegradeToMissWithoutHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	if dir, ok := machineCacheDir(); ok {
		t.Fatalf("machineCacheDir() = %q, want no location when no home resolves", dir)
	}
	if path, ok := versionCachePath("archdtop"); ok {
		t.Fatalf("versionCachePath() = %q, want no location when no home resolves", path)
	}
	if got, ok := readMachineCacheCampaigns("archdtop"); ok {
		t.Errorf("readMachineCacheCampaigns() = %v, true; want a miss", got)
	}
	if _, ok := readMachineVersionCache("archdtop"); ok {
		t.Error("readMachineVersionCache() reported a hit with no cache location")
	}

	// Writes must be no-ops rather than panics or relative-path writes.
	writeMachineCacheCampaigns("archdtop", []string{"campaign"})
	writeMachineSnapshotCampaigns("archdtop", []string{"campaign"})
	writeMachineVersionCache("archdtop", "1.2.3", "deadbeef")
}
