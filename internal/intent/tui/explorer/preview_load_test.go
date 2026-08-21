package explorer

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func previewNavModel(t *testing.T) Model {
	t.Helper()
	m := makeTestModel(5, 0)
	m.showPreview = true
	m.cursorGroup = 0
	m.cursorItem = 0
	m.groups[0].Expanded = true
	m.groups[0].Intents[0].Content = "preview body for first inbox item"
	m.previewPane.SetSize(40, 20)
	return m
}

func TestRapidScrollDoesNotApplyPreviewInUpdate(t *testing.T) {
	m := previewNavModel(t)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("j/k with preview visible must schedule an async preview load")
	}
	if m.previewSeq != 1 {
		t.Fatalf("previewSeq = %d, want 1", m.previewSeq)
	}
	if m.previewPane.HasContent() {
		t.Fatal("navigation Update must not render markdown synchronously")
	}
	if m.previewPane.Title() != "" {
		t.Fatalf("preview title applied during scroll: %q", m.previewPane.Title())
	}
}

func TestRapidScrollThenQuitReturnsQuitImmediately(t *testing.T) {
	m := previewNavModel(t)

	scheduled := 0
	for range 12 {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = updated.(Model)
		if cmd != nil {
			scheduled++
		}
	}
	if scheduled == 0 {
		t.Fatal("expected at least one debounce cmd while preview is visible")
	}
	if m.previewPane.HasContent() {
		t.Fatal("rapid j must not apply preview content before debounce/load")
	}

	seq := m.previewSeq
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(Model)

	if !m.quitting {
		t.Fatal("q must set quitting without waiting for preview renders")
	}
	if m.previewSeq <= seq {
		t.Fatal("q must bump previewSeq so in-flight loads are stale")
	}
	if cmd == nil {
		t.Fatal("q must return tea.Quit")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("q cmd produced %T, want tea.QuitMsg", msg)
	}
}

func TestPreviewDebounceIgnoresStaleGeneration(t *testing.T) {
	m := previewNavModel(t)
	m.previewSeq = 7

	updated, cmd := m.Update(previewDebounceMsg{seq: 6, title: "stale", body: "nope", width: 40})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("stale debounce must not start a load")
	}
	if m.previewPane.HasContent() {
		t.Fatal("stale debounce must not apply preview content")
	}
}

func TestPreviewDebounceMatchingGenerationLoads(t *testing.T) {
	m := previewNavModel(t)
	m.previewSeq = 3

	updated, cmd := m.Update(previewDebounceMsg{
		seq:   3,
		title: "current",
		body:  "hello",
		width: 40,
	})
	if cmd == nil {
		t.Fatal("matching debounce must return loadPreviewCmd")
	}
	_ = updated
}

func TestPreviewLoadedAppliesMatchingGeneration(t *testing.T) {
	m := previewNavModel(t)
	m.previewSeq = 4

	updated, cmd := m.Update(previewLoadedMsg{
		seq:      4,
		title:    "Loaded Title",
		raw:      "raw",
		rendered: "RENDERED-MARKDOWN",
	})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("loaded handler should not return a follow-up cmd")
	}
	if m.previewPane.Title() != "Loaded Title" {
		t.Fatalf("title = %q, want Loaded Title", m.previewPane.Title())
	}
	if !m.previewPane.HasContent() {
		t.Fatal("expected preview content after matching loaded msg")
	}
	if !strings.Contains(m.previewPane.View(), "Loaded Title") {
		t.Fatalf("view missing title: %q", m.previewPane.View())
	}
}

func TestPreviewLoadedIgnoresStaleGeneration(t *testing.T) {
	m := previewNavModel(t)
	m.previewSeq = 9

	updated, _ := m.Update(previewLoadedMsg{
		seq:      8,
		title:    "stale-title",
		rendered: "stale-body",
	})
	m = updated.(Model)
	if m.previewPane.Title() == "stale-title" {
		t.Fatal("stale loaded msg applied preview")
	}
	if m.previewPane.HasContent() {
		t.Fatal("stale loaded msg must be ignored")
	}
}

func TestPreviewLoadedIgnoredWhileQuitting(t *testing.T) {
	m := previewNavModel(t)
	m.previewSeq = 1
	m.quitting = true

	updated, _ := m.Update(previewLoadedMsg{
		seq:      1,
		title:    "late",
		rendered: "late-body",
	})
	m = updated.(Model)
	if m.previewPane.HasContent() {
		t.Fatal("loaded msg after quit must be ignored")
	}
}

func TestHiddenPreviewDoesNotScheduleLoad(t *testing.T) {
	m := previewNavModel(t)
	m.showPreview = false

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("hidden preview must not schedule a load")
	}
	if m.previewSeq != 0 {
		t.Fatalf("previewSeq = %d, want 0 when preview is hidden", m.previewSeq)
	}
}

func TestLoadPreviewCmdMissingFileReturnsErrorText(t *testing.T) {
	cmd := loadPreviewCmd(previewDebounceMsg{
		seq:   1,
		title: "Missing",
		path:  "/this/path/does/not/exist/camp-preview-load.md",
		width: 40,
	})
	got := cmd()
	msg, ok := got.(previewLoadedMsg)
	if !ok {
		t.Fatalf("loadPreviewCmd returned %T", got)
	}
	if msg.seq != 1 {
		t.Fatalf("seq = %d, want 1", msg.seq)
	}
	if !strings.Contains(msg.rendered, "Could not read file") {
		t.Fatalf("rendered = %q, want read error", msg.rendered)
	}
}
