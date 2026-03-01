//go:build integration

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupCampaignWithDirtyRef creates a campaign root with a submodule
// where the submodule HEAD differs from what the campaign root recorded.
func setupCampaignWithDirtyRef(t *testing.T) string {
	t.Helper()

	campRoot, subPath := setupSyncTestRepo(t)

	// Advance the submodule to create a dirty ref
	os.WriteFile(filepath.Join(subPath, "change.txt"), []byte("new"), 0644)
	run(t, "git", "-C", subPath, "add", "-A")
	run(t, "git", "-C", subPath, "commit", "-m", "advance submodule")

	return campRoot
}

func TestIntegration_Status_DefaultHidesRefs(t *testing.T) {
	campRoot := setupCampaignWithDirtyRef(t)

	// With --ignore-submodules=all (simulates default camp status behavior)
	output := run(t, "git", "-C", campRoot, "status", "--short", "--ignore-submodules=all")

	if strings.Contains(output, "projects/test-project") {
		t.Errorf("default status should hide submodule ref changes, got: %s", output)
	}
}

func TestIntegration_Status_ShowRefsReveals(t *testing.T) {
	campRoot := setupCampaignWithDirtyRef(t)

	// Without --ignore-submodules (simulates --show-refs behavior)
	output := run(t, "git", "-C", campRoot, "status", "--short")

	if !strings.Contains(output, "projects/test-project") {
		t.Errorf("--show-refs status should show submodule ref changes, got: %s", output)
	}
}
