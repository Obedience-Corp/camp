package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/ui/uitest"
	tea "github.com/charmbracelet/bubbletea"
)

func TestArtifactExplorerFilteringAndActions(t *testing.T) {
	m := newArtifactModel(context.Background(), "/camp", true)
	updated, _ := m.Update(artifactLoadedMsg{files: []artifacts.InventoryFile{{Path: "media/clip.mp4"}, {Path: "media/take two.mov"}}})
	m = updated.(artifactModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(artifactModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("take")})
	m = updated.(artifactModel)
	if len(m.visible) != 1 || m.visible[0].Path != "media/take two.mov" {
		t.Fatalf("filter: %+v", m.visible)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(artifactModel)
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if cmd == nil || !updated.(artifactModel).busy {
		t.Fatal("go must validate asynchronously")
	}
	updated, cmd = m.Update(artifactActionMsg{action: "go", path: "/camp/media"})
	if cmd == nil || updated.(artifactModel).gotoPath != "/camp/media" {
		t.Fatal("missing navigation result")
	}
	m.gotoEnabled = false
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if cmd != nil || !strings.Contains(updated.(artifactModel).status, "shell") {
		t.Fatal("missing shell integration hint")
	}
	m.input.SetValue("missing")
	m.filter()
	if len(m.visible) != 0 || !strings.Contains(m.View(), "No matching artifacts") {
		t.Fatal("no-match state")
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("empty selection must not open")
	}
}

func TestArtifactExplorerResponsive(t *testing.T) {
	for _, dims := range [][2]int{{120, 40}, {96, 24}, {80, 24}, {40, 12}, {24, 8}, {12, 4}} {
		m := newArtifactModel(context.Background(), "/camp", true)
		m.width, m.height = dims[0], dims[1]
		m.loading = false
		for i := 0; i < 100; i++ {
			m.all = append(m.all, artifacts.InventoryFile{Path: fmt.Sprintf("media/very-long-folder/视频-%03d-with-a-long-name.mp4", i), Root: "media", Size: 123456})
		}
		m.filter()
		m.cursor = 99
		uitest.AssertBounded(t, m.View(), m.width, m.height)
		if m.width >= 24 && !strings.Contains(m.View(), "q quit") {
			t.Fatal("quit help clipped")
		}
		m.status = "opener failed"
		m.statusErr = true
		m.filtering = true
		uitest.AssertBounded(t, m.View(), m.width, m.height)
	}
}

func TestArtifactExplorerFailureAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := newArtifactModel(ctx, "/nonexistent", true)
	result := m.load().(artifactLoadedMsg)
	if result.err != context.Canceled {
		t.Fatalf("cancellation: %v", result.err)
	}
	updated, _ := m.Update(result)
	if updated.(artifactModel).loading || !updated.(artifactModel).statusErr {
		t.Fatal("load failure must be visible")
	}
	if strings.Contains(artifactDisplay("media/bad\x1b[2J\nname"), "\x1b") {
		t.Fatal("filename can control terminal")
	}
}
