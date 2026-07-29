package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupMachinesFile(t *testing.T, n int) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "machines.yaml")
	var b strings.Builder
	b.WriteString("version: 1\nmachines:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  - id: m%d\n    host: m%d.ts.net\n    auth_method: tailscale-ssh\n", i, i)
	}
	if n == 0 {
		b.Reset()
		b.WriteString("version: 1\nmachines: []\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CAMP_MACHINES_PATH", path)
}

func TestMaybeEmitMachinesHint(t *testing.T) {
	origTTY := stderrIsTTY
	t.Cleanup(func() { stderrIsTTY = origTTY })

	t.Run("suppresses when stderr not tty", func(t *testing.T) {
		stderrIsTTY = func() bool { return false }
		setupMachinesFile(t, 2)
		var buf bytes.Buffer
		maybeEmitMachinesHint(&buf, "table")
		if buf.Len() != 0 {
			t.Errorf("expected no hint for non-TTY stderr, got %q", buf.String())
		}
	})

	t.Run("suppresses for json format", func(t *testing.T) {
		stderrIsTTY = func() bool { return true }
		setupMachinesFile(t, 1)
		var buf bytes.Buffer
		maybeEmitMachinesHint(&buf, "json")
		if buf.Len() != 0 {
			t.Errorf("expected no hint for json, got %q", buf.String())
		}
	})

	t.Run("emits when tty and machines present", func(t *testing.T) {
		stderrIsTTY = func() bool { return true }
		setupMachinesFile(t, 2)
		var buf bytes.Buffer
		maybeEmitMachinesHint(&buf, "table")
		if !strings.Contains(buf.String(), "2 machine(s) configured") {
			t.Errorf("hint missing count: %q", buf.String())
		}
		if !strings.Contains(buf.String(), "camp list --remote") {
			t.Errorf("hint missing --remote guidance: %q", buf.String())
		}
	})

	t.Run("silent when no machines", func(t *testing.T) {
		stderrIsTTY = func() bool { return true }
		setupMachinesFile(t, 0)
		var buf bytes.Buffer
		maybeEmitMachinesHint(&buf, "table")
		if buf.Len() != 0 {
			t.Errorf("expected no hint with empty fleet, got %q", buf.String())
		}
	})
}
