package main

import (
	"bytes"
	"encoding/json"
	"strings"
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

func TestVersionCommand_BundleLine(t *testing.T) {
	original := version.Bundle
	t.Cleanup(func() { version.Bundle = original })

	tests := []struct {
		name       string
		bundle     string
		wantBundle bool
	}{
		{name: "standalone build prints no bundle line", bundle: "", wantBundle: false},
		{name: "festival build names the suite version", bundle: "v0.2.17", wantBundle: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version.Bundle = tt.bundle

			cmd, out := newVersionTestCommand(t)
			if err := runVersion(cmd, nil); err != nil {
				t.Fatalf("runVersion() error = %v", err)
			}

			lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
			if !strings.HasPrefix(lines[0], "camp ") {
				t.Fatalf("first line = %q; want a camp <version> line", lines[0])
			}

			if !tt.wantBundle {
				for _, line := range lines {
					if strings.HasPrefix(line, "bundle:") {
						t.Fatalf("unexpected bundle line %q\nfull output:\n%s", line, out.String())
					}
				}
				return
			}

			// The plugin update checker reads the suite version off its own
			// line, directly under the camp version.
			want := "bundle: festival " + tt.bundle
			if len(lines) < 2 {
				t.Fatalf("output has %d line(s); want the bundle line second\nfull output:\n%s", len(lines), out.String())
			}
			if lines[1] != want {
				t.Fatalf("second line = %q; want %q\nfull output:\n%s", lines[1], want, out.String())
			}
		})
	}
}

func TestVersionCommand_JSONBundleField(t *testing.T) {
	original := version.Bundle
	t.Cleanup(func() { version.Bundle = original })

	tests := []struct {
		name       string
		bundle     string
		wantBundle bool
	}{
		{name: "standalone build omits the bundle key", bundle: "", wantBundle: false},
		{name: "festival build emits the bundle key", bundle: "v0.2.17", wantBundle: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version.Bundle = tt.bundle

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

			got, ok := payload["bundle"]
			if ok != tt.wantBundle {
				t.Fatalf("bundle key present = %v; want %v\nraw: %s", ok, tt.wantBundle, out.String())
			}
			if tt.wantBundle && got != tt.bundle {
				t.Fatalf("bundle = %q; want %q", got, tt.bundle)
			}
		})
	}
}

func TestVersionCommand_ShortIgnoresBundle(t *testing.T) {
	original := version.Bundle
	t.Cleanup(func() { version.Bundle = original })
	version.Bundle = "v0.2.17"

	cmd, out := newVersionTestCommand(t)
	if err := cmd.Flags().Set("short", "true"); err != nil {
		t.Fatalf("set short flag: %v", err)
	}
	if err := runVersion(cmd, nil); err != nil {
		t.Fatalf("runVersion() error = %v", err)
	}

	if got := strings.TrimRight(out.String(), "\n"); got != version.Version {
		t.Fatalf("--short output = %q; want %q", got, version.Version)
	}
}
