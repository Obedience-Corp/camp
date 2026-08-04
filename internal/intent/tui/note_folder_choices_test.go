package tui

import (
	"reflect"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestNoteFolderChoicesPinsRootAndSortsUserFoldersByRecency(t *testing.T) {
	base := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	folders := []intent.NoteFolder{
		{Status: intent.StatusNote, Name: "Notes", ModTime: base.Add(3 * time.Hour)},
		{Status: intent.StatusNoteArchived, Name: "Archived", Reserved: true},
		{Status: intent.Status("notes/older"), Name: "Older", ModTime: base},
		{Status: intent.Status("notes/recent/nested"), Name: "Nested", ModTime: base.Add(2 * time.Hour)},
		{Status: intent.Status("notes/recent"), Name: "Recent", ModTime: base.Add(time.Hour)},
	}

	got := NoteFolderChoices(folders)
	want := []NoteFolderChoice{
		{Label: "Notes (default)"},
		{Rel: "recent/nested", Label: "recent/nested"},
		{Rel: "recent", Label: "recent"},
		{Rel: "older", Label: "older"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("choices = %#v, want %#v", got, want)
	}
}

func TestNoteFolderChoicesHidesDestinationWithoutUserFolders(t *testing.T) {
	got := NoteFolderChoices([]intent.NoteFolder{
		{Status: intent.StatusNote, Name: "Notes"},
		{Status: intent.StatusNoteMeetings, Name: "Meetings", Reserved: true},
		{Status: intent.StatusNoteArchived, Name: "Archived", Reserved: true},
	})
	if got != nil {
		t.Fatalf("choices = %#v, want nil", got)
	}
}
