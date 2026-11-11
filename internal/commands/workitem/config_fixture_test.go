package workitem

import (
	"path/filepath"
	"testing"
)

// pinGlobalConfig points the global config at a tracked fixture, so tests
// never read or write the host's. dungeon_hidden is unset there and takes its
// default. ledger_writer_id is preset so promote's ledger writes do not save one.
func pinGlobalConfig(t *testing.T) {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "global-config"))
	if err != nil {
		t.Fatalf("resolving testdata/global-config: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
}
