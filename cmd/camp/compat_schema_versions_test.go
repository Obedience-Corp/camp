package main

import "testing"

// TestStatusAllSchemaVersionIsFrozen pins the one published schema string that
// lives in package main. The rest are pinned in internal/compat, which cannot
// import this package.
func TestStatusAllSchemaVersionIsFrozen(t *testing.T) {
	if StatusAllJSONVersion != "status-all/v1alpha1" {
		t.Fatalf("camp status all schema version: got %q, want %q (docs/json-contracts.md)",
			StatusAllJSONVersion, "status-all/v1alpha1")
	}
}

// TestSwitchSchemaVersionIsFrozen pins the switch contract's version string.
// The shell integration parses that payload on every `csw`.
func TestSwitchSchemaVersionIsFrozen(t *testing.T) {
	if switchSchemaVersion != "camp-switch/v1" {
		t.Fatalf("camp switch schema version: got %q, want %q", switchSchemaVersion, "camp-switch/v1")
	}
}
