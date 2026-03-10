package dungeon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewDocsBrowserModel_RequiresDocsSubdirectories(t *testing.T) {
	root, err := os.MkdirTemp("", "docs-browser-empty-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(root)

	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatalf("failed to create docs root: %v", err)
	}

	_, err = newDocsBrowserModel("note.md", root)
	if err == nil {
		t.Fatal("expected error for empty docs root")
	}
	if !errors.Is(err, ErrInvalidDocsDestination) {
		t.Fatalf("expected ErrInvalidDocsDestination, got %v", err)
	}
}

func TestDocsBrowserModel_TabCyclesAtRoot(t *testing.T) {
	root := makeDocsBrowserTree(t, []string{
		"architecture",
		"guides",
		"reference/cli",
	})

	model := mustNewDocsBrowserModel(t, "note.md", root)
	assertCurrentPath(t, model, "architecture")

	model = updateDocsBrowser(t, model, tea.KeyMsg{Type: tea.KeyTab})
	assertCurrentPath(t, model, "guides")

	model = updateDocsBrowser(t, model, tea.KeyMsg{Type: tea.KeyTab})
	assertCurrentPath(t, model, "reference/")

	model = updateDocsBrowser(t, model, tea.KeyMsg{Type: tea.KeyTab})
	assertCurrentPath(t, model, "architecture")
}

func TestDocsBrowserModel_ShiftTabCyclesBackward(t *testing.T) {
	root := makeDocsBrowserTree(t, []string{
		"architecture",
		"guides",
		"reference/cli",
	})

	model := mustNewDocsBrowserModel(t, "note.md", root)
	model = updateDocsBrowser(t, model, tea.KeyMsg{Type: tea.KeyShiftTab})

	assertCurrentPath(t, model, "reference/")
}

func TestDocsBrowserModel_EnterLeafSelectsPath(t *testing.T) {
	root := makeDocsBrowserTree(t, []string{
		"architecture",
		"reference/cli",
	})

	model := mustNewDocsBrowserModel(t, "note.md", root)
	model = updateDocsBrowser(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if !model.done {
		t.Fatal("expected model to finish on leaf selection")
	}
	if model.cancelled {
		t.Fatal("expected leaf selection, got cancel")
	}
	if model.selected != "architecture" {
		t.Fatalf("selected = %q, want %q", model.selected, "architecture")
	}
}

func TestDocsBrowserModel_EnterDescendsAndEscRestoresParentSelection(t *testing.T) {
	root := makeDocsBrowserTree(t, []string{
		"architecture",
		"business/articles",
		"business/pricing",
		"guides",
	})

	model := mustNewDocsBrowserModel(t, "note.md", root)
	model = updateDocsBrowser(t, model, tea.KeyMsg{Type: tea.KeyTab})
	assertCurrentPath(t, model, "business/")

	model = updateDocsBrowser(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if len(model.levels) != 2 {
		t.Fatalf("expected 2 levels after descend, got %d", len(model.levels))
	}
	if got := model.currentLevel().prefix; got != "business" {
		t.Fatalf("prefix = %q, want %q", got, "business")
	}
	assertCurrentPath(t, model, "business/articles")

	model = updateDocsBrowser(t, model, tea.KeyMsg{Type: tea.KeyTab})
	assertCurrentPath(t, model, "business/pricing")

	model = updateDocsBrowser(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if len(model.levels) != 1 {
		t.Fatalf("expected to return to root level, got %d levels", len(model.levels))
	}
	assertCurrentPath(t, model, "business/")
}

func TestDocsBrowserModel_EscAtRootCancels(t *testing.T) {
	root := makeDocsBrowserTree(t, []string{
		"architecture",
	})

	model := mustNewDocsBrowserModel(t, "note.md", root)
	model = updateDocsBrowser(t, model, tea.KeyMsg{Type: tea.KeyEsc})

	if !model.done {
		t.Fatal("expected model to finish on root escape")
	}
	if !model.cancelled {
		t.Fatal("expected model to be cancelled on root escape")
	}
}

func makeDocsBrowserTree(t *testing.T, dirs []string) string {
	t.Helper()

	root, err := os.MkdirTemp("", "docs-browser-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(root, "docs", filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}

	return root
}

func mustNewDocsBrowserModel(t *testing.T, itemName, campaignRoot string) docsBrowserModel {
	t.Helper()

	model, err := newDocsBrowserModel(itemName, campaignRoot)
	if err != nil {
		t.Fatalf("newDocsBrowserModel() error = %v", err)
	}
	return model
}

func updateDocsBrowser(t *testing.T, model docsBrowserModel, msg tea.Msg) docsBrowserModel {
	t.Helper()

	updated, _ := model.Update(msg)
	nextModel, ok := updated.(docsBrowserModel)
	if !ok {
		t.Fatalf("expected docsBrowserModel, got %T", updated)
	}
	return nextModel
}

func assertCurrentPath(t *testing.T, model docsBrowserModel, want string) {
	t.Helper()

	entry, ok := model.currentEntry()
	if !ok {
		t.Fatal("expected current entry")
	}
	if got := entry.displayPath(); got != want {
		t.Fatalf("current entry = %q, want %q", got, want)
	}
}
