//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/sys/windows"
)

func TestArtifactShellErrorExplainsAssociation(t *testing.T) {
	for _, err := range []error{
		artifactShellError(31, nil),
		artifactShellError(0, windows.ERROR_NO_ASSOCIATION),
	} {
		if !strings.Contains(err.Error(), "no application is associated") {
			t.Fatalf("missing association explanation: %v", err)
		}
	}
}

func TestArtifactExplorerWindowsLaunchStatus(t *testing.T) {
	previous := artifactOpenWindows
	t.Cleanup(func() { artifactOpenWindows = previous })
	var launched string
	artifactOpenWindows = func(path string) error {
		launched = path
		return nil
	}
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	m := newArtifactModel(context.Background(), root, true)
	updated, _ := m.Update(artifactLoadedMsg{files: []artifacts.InventoryFile{{Path: "."}}})
	m = updated.(artifactModel)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil || !updated.(artifactModel).busy {
		t.Fatal("open must run outside the TUI update")
	}
	result, ok := cmd().(artifactActionMsg)
	if !ok || result.err != nil || result.wait != nil || launched != root {
		t.Fatalf("Windows launch result: %#v, path=%q", result, launched)
	}
	updated, wait := updated.(artifactModel).Update(result)
	m = updated.(artifactModel)
	if wait != nil || m.busy || m.statusErr || m.status != "Opened "+filepath.Base(root) {
		t.Fatalf("launch must report success and release actions: busy=%v status=%q", m.busy, m.status)
	}
	_, next := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if next == nil {
		t.Fatal("navigation unavailable after open")
	}

	artifactOpenWindows = func(string) error { return artifactShellError(31, nil) }
	result = artifactAction(context.Background(), root, ".", "open")().(artifactActionMsg)
	if result.err == nil {
		t.Fatal("missing association reported success")
	}
	updated, _ = m.Update(result)
	m = updated.(artifactModel)
	if !m.statusErr || !strings.Contains(m.status, "no application is associated") {
		t.Fatalf("missing association not visible in TUI: %q", m.status)
	}

	launched = ""
	result = artifactAction(context.Background(), root, "missing-artifact", "open")().(artifactActionMsg)
	if result.err == nil || !errors.Is(result.err, os.ErrNotExist) || launched != "" {
		t.Fatalf("stale path must fail before Shell launch: %#v, path=%q", result, launched)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	result = artifactAction(canceled, root, ".", "open")().(artifactActionMsg)
	if !errors.Is(result.err, context.Canceled) || launched != "" {
		t.Fatalf("cancellation must stop Shell launch: %#v, path=%q", result, launched)
	}
}
