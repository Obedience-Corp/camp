//go:build windows && container_fs

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	tea "github.com/charmbracelet/bubbletea"
)

func TestArtifactWindowsNativeShellLaunch(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "opened.txt")
	scriptName := "open [artifact].cmd"
	script := filepath.Join(root, scriptName)
	// Publish only after echo closes the file: ShellExecute returns before the
	// handler finishes, and cmd redirection creates an empty file first.
	content := "@echo off\r\necho opened>\"" + marker + ".pending\"\r\n" +
		"move /y \"" + marker + ".pending\" \"" + marker + "\" >nul\r\n"
	if err := os.WriteFile(script, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	m := newArtifactModel(context.Background(), root, true)
	updated, _ := m.Update(artifactLoadedMsg{files: []artifacts.InventoryFile{{Path: scriptName}}})
	m = updated.(artifactModel)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if cmd == nil || !updated.(artifactModel).busy {
		t.Fatal("open did not schedule a background action")
	}
	result, ok := cmd().(artifactActionMsg)
	if !ok || result.err != nil || result.wait != nil {
		t.Fatalf("Windows Shell launch: %#v", result)
	}
	updated, wait := updated.(artifactModel).Update(result)
	m = updated.(artifactModel)
	if wait != nil || m.busy || m.statusErr || m.status != "Opened "+scriptName {
		t.Fatalf("TUI launch status: busy=%v status=%q", m.busy, m.status)
	}
	if !strings.Contains(m.View(), "Opened "+scriptName) {
		t.Fatalf("launch status absent from rendered explorer: %q", m.View())
	}
	_, next := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if next == nil {
		t.Fatal("TUI actions unavailable after launch")
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		bytes, err := os.ReadFile(marker)
		if err == nil {
			if strings.TrimSpace(string(bytes)) != "opened" {
				t.Fatalf("handler marker content: %q", bytes)
			}
			break
		}
		if !os.IsNotExist(err) || time.Now().After(deadline) {
			t.Fatalf("associated handler did not write marker: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestArtifactWindowsNativeMissingAssociation(t *testing.T) {
	root := t.TempDir()
	name := "no-handler.cf0006-unassociated"
	if err := os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	result := artifactAction(context.Background(), root, name, "open")().(artifactActionMsg)
	if result.err == nil || result.wait != nil {
		t.Fatalf("unassociated file appeared open: %#v", result)
	}
	m := newArtifactModel(context.Background(), root, true)
	updated, _ := m.Update(result)
	m = updated.(artifactModel)
	if !m.statusErr || !strings.Contains(m.status, "associated") || m.quitting {
		t.Fatalf("missing association was not reported in the explorer: %q", m.status)
	}
	if !strings.Contains(m.View(), "associated") {
		t.Fatalf("association failure absent from rendered explorer: %q", m.View())
	}
}
