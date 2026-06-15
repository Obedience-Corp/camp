package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Obedience-Corp/camp/internal/version"
	"github.com/spf13/cobra"
)

func TestVersionCommand_JSONContract(t *testing.T) {
	cmd, out := newVersionTestCommand(t)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set json flag: %v", err)
	}

	if err := runVersion(cmd, nil); err != nil {
		t.Fatalf("runVersion() error = %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}

	if got := payload["schema_version"]; got != version.SchemaVersion {
		t.Fatalf("schema_version = %q, want %q", got, version.SchemaVersion)
	}
	if got := payload["profile"]; got != version.Profile {
		t.Fatalf("profile = %q, want %q", got, version.Profile)
	}
	if payload["build_date"] == "" {
		t.Fatal("build_date is empty")
	}
	if payload["go_version"] == "" {
		t.Fatal("go_version is empty")
	}
	if _, ok := payload["buildDate"]; ok {
		t.Fatal("legacy buildDate key should not be emitted")
	}
	if _, ok := payload["goVersion"]; ok {
		t.Fatal("legacy goVersion key should not be emitted")
	}
}

func TestVersionCommand_JSONWinsOverShort(t *testing.T) {
	cmd, out := newVersionTestCommand(t)
	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set json flag: %v", err)
	}
	if err := cmd.Flags().Set("short", "true"); err != nil {
		t.Fatalf("set short flag: %v", err)
	}

	if err := runVersion(cmd, nil); err != nil {
		t.Fatalf("runVersion() error = %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, out.String())
	}
	if got := payload["version"]; got == "" {
		t.Fatal("version is empty")
	}
}

func TestVersionCommand_UsesRunE(t *testing.T) {
	if versionCmd.RunE == nil {
		t.Fatal("versionCmd.RunE is nil")
	}
	if versionCmd.Run != nil {
		t.Fatal("versionCmd.Run should be nil")
	}
}

func newVersionTestCommand(t *testing.T) (*cobra.Command, *bytes.Buffer) {
	t.Helper()

	cmd := &cobra.Command{Use: "version"}
	cmd.Flags().BoolP("short", "s", false, "show only version number")
	cmd.Flags().Bool("json", false, "output as JSON")

	out := new(bytes.Buffer)
	cmd.SetOut(out)
	return cmd, out
}
