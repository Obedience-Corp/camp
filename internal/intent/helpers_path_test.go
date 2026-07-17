package intent

import "testing"

// mustIntentPath resolves an intent's path, failing the test if the dungeon
// spelling cannot be resolved.
func mustIntentPath(t *testing.T, svc *IntentService, status Status, id string) string {
	t.Helper()
	path, err := svc.getIntentPath(status, id)
	if err != nil {
		t.Fatalf("getIntentPath(%q, %q): %v", status, id, err)
	}
	return path
}

// mustStatusDir resolves a status directory, failing the test if the dungeon
// spelling cannot be resolved.
func mustStatusDir(t *testing.T, svc *IntentService, status Status) string {
	t.Helper()
	dir, err := svc.statusDir(status)
	if err != nil {
		t.Fatalf("statusDir(%q): %v", status, err)
	}
	return dir
}
