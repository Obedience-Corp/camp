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

func TestNewActionMenu_UserFolderNoteIsActive(t *testing.T) {
	m := NewActionMenu(&intent.Intent{ID: "c", Status: intent.Status("notes/reading")})

	for _, action := range []string{"move", "convert", "archive"} {
		if !itemEnabled(m, action) {
			t.Errorf("user-folder note menu should enable %s", action)
		}
	}
	if itemEnabled(m, "restore") {
		t.Error("user-folder note menu should not enable restore")
	}
}

func TestActionMenuOffersCopyID(t *testing.T) {
	tests := []struct {
		name   string
		i      *intent.Intent
		enable bool
	}{
		{"intent with id", &intent.Intent{ID: "inbox-0", Status: intent.StatusInbox}, true},
		{"note with id", &intent.Intent{ID: "note-0", Status: intent.StatusNote}, true},
		{"intent without id", &intent.Intent{Status: intent.StatusInbox}, false},
		{"note without id", &intent.Intent{Status: intent.StatusNote}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			menu := NewActionMenu(tt.i)
			var found bool
			for _, item := range menu.items {
				if item.Action != "copy-id" {
					continue
				}
				found = true
				if item.Enabled != tt.enable {
					t.Errorf("Copy ID enabled = %v, want %v", item.Enabled, tt.enable)
				}
			}
			if !found {
				t.Fatal("action menu has no Copy ID entry")
			}
		})
	}
}
