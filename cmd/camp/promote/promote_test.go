package promote

import "testing"

func TestShelveCommandIsHiddenAlias(t *testing.T) {
	if !Cmd.Hidden {
		t.Fatal("shelve command should be Hidden (deprecated alias)")
	}
	if Cmd.Use != "shelve <status>" {
		t.Fatalf("Use = %q, want %q", Cmd.Use, "shelve <status>")
	}
	if Cmd.RunE == nil {
		t.Fatal("shelve command should keep a RunE delegate")
	}
}
