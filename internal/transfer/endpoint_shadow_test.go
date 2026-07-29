package transfer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The shadow note is the only thing that tells an operator their spec was read
// as a machine when it could also name a campaign. ParseEndpointDefault passed
// a nil campaign lookup, so Shadowed was always false and the note was
// unreachable in production; the feature only ever ran in tests that injected
// their own lookup.
func TestParseEndpointDefaultDetectsARealShadow(t *testing.T) {
	dir := t.TempDir()

	machinesPath := filepath.Join(dir, "machines.yaml")
	if err := os.WriteFile(machinesPath, []byte(
		"version: 1\nmachines:\n  - id: archdtop\n    host: archdtop.example.ts.net\n    user: lance\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAMP_MACHINES_PATH", machinesPath)

	registryPath := filepath.Join(dir, "registry.json")
	if err := os.WriteFile(registryPath, []byte(
		`{"version":1,"campaigns":{"archdtop":{"name":"archdtop","path":"`+dir+`","status":"active"}}}`),
		0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAMP_REGISTRY_PATH", registryPath)

	ep, err := ParseEndpointDefault(context.Background(), "archdtop:demo:notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if ep.Machine != "archdtop" {
		t.Fatalf("machine = %q, want the machine reading to win", ep.Machine)
	}
	if !ep.Shadowed {
		t.Error("a head that is both a machine and a campaign must be reported as shadowed")
	}
}

// The check must not fire on an unambiguous spec, or every transfer would carry
// a note about nothing.
func TestParseEndpointDefaultQuietWhenOnlyAMachineMatches(t *testing.T) {
	dir := t.TempDir()
	machinesPath := filepath.Join(dir, "machines.yaml")
	if err := os.WriteFile(machinesPath, []byte(
		"version: 1\nmachines:\n  - id: archdtop\n    host: archdtop.example.ts.net\n    user: lance\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAMP_MACHINES_PATH", machinesPath)
	t.Setenv("CAMP_REGISTRY_PATH", filepath.Join(dir, "absent.json"))

	ep, err := ParseEndpointDefault(context.Background(), "archdtop:demo:notes.md")
	if err != nil {
		t.Fatal(err)
	}
	if ep.Shadowed {
		t.Error("no campaign of that name exists; nothing is shadowed")
	}
}
