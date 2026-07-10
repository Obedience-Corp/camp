package tui

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/intent"
)

// itemEnabled reports whether the menu offers action with Enabled=true.
func itemEnabled(m ActionMenu, action string) bool {
	for _, item := range m.items {
		if item.Action == action {
			return item.Enabled
		}
	}
	return false
}

func hasAction(m ActionMenu, action string) bool {
	for _, item := range m.items {
		if item.Action == action {
			return true
		}
	}
	return false
}

func TestNewActionMenu_ActiveNoteOffersArchiveNotRestore(t *testing.T) {
	m := NewActionMenu(&intent.Intent{ID: "a", Status: intent.StatusNote})

	if !itemEnabled(m, "archive") {
		t.Error("active note menu should enable Archive")
	}
	if !hasAction(m, "restore") {
		t.Fatal("note menu missing Restore item")
	}
	if itemEnabled(m, "restore") {
		t.Error("active note menu should not enable Restore")
	}
	if !itemEnabled(m, "convert") {
		t.Error("active note menu should enable Convert")
	}
}

func TestNewActionMenu_ArchivedNoteOffersRestoreNotArchive(t *testing.T) {
	m := NewActionMenu(&intent.Intent{ID: "b", Status: intent.StatusNoteArchived})

	if itemEnabled(m, "archive") {
		t.Error("archived note menu should not enable Archive")
	}
	if !itemEnabled(m, "restore") {
		t.Error("archived note menu should enable Restore")
	}
	if itemEnabled(m, "convert") {
		t.Error("archived note menu should not enable Convert")
	}
}
