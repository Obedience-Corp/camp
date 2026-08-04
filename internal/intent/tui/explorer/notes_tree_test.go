package explorer

import (
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/intent"
)

func TestBuildNotesTree_CollapsedDefaultAndNestedReachable(t *testing.T) {
	items := []*intent.Intent{
		{ID: "root", Title: "root note", Status: intent.StatusNote},
		{ID: "nested", Title: "nested paper", Status: intent.Status("notes/reading/papers")},
		{ID: "inbox", Title: "inbox item", Status: intent.StatusInbox},
	}

	groups := groupExplorerItemsByStatus(items, nil, false, nil, false)

	// Inbox, Ready, Active, then Notes...
	if groups[0].Name != "Inbox" || groups[2].Name != "Active" {
		t.Fatalf("unexpected top groups: %q %q %q", groups[0].Name, groups[1].Name, groups[2].Name)
	}
	notes := groups[3]
	if notes.Name != "Notes" || notes.Expanded {
		t.Fatalf("Notes default = name=%q expanded=%v, want collapsed Notes", notes.Name, notes.Expanded)
	}
	if notes.DescendantCount != 2 {
		t.Fatalf("Notes DescendantCount = %d, want 2", notes.DescendantCount)
	}

	// Nested note present in tree under expanded path.
	fold := map[string]bool{
		"notes": true, "notes/reading": true, "notes/reading/papers": true,
	}
	expanded := groupExplorerItemsByStatus(items, nil, false, fold, false)
	var foundNested bool
	for _, g := range expanded {
		for _, it := range g.Intents {
			if it.ID == "nested" {
				foundNested = true
			}
		}
	}
	if !foundNested {
		t.Fatal("nested note not present when path expanded")
	}

	// Render collapsed: child headers hidden.
	m := NewModel(t.Context(), nil, nil, "/tmp", "/tmp", "id", "", nil)
	m.ready = true
	m.width = 120
	m.height = 30
	m.listHeight = 25
	m.filteredIntents = items
	m.groups = groups
	view := m.buildMainView()
	if !strings.Contains(view, "Notes") {
		t.Fatalf("view missing Notes:\n%s", view)
	}
	if strings.Contains(view, "Reading") || strings.Contains(view, "nested paper") {
		t.Fatalf("collapsed Notes should hide children:\n%s", view)
	}
}

func TestFilterOmitsEmptyNoteFoldersAndExpandsMatches(t *testing.T) {
	items := []*intent.Intent{
		{ID: "nested", Title: "unique-zebra-note", Status: intent.Status("notes/reading/papers")},
		{ID: "inbox", Title: "other", Status: intent.StatusInbox},
	}
	// Filtered to only the nested note.
	filtered := []*intent.Intent{items[0]}
	groups := groupExplorerItemsByStatus(filtered, nil, false, nil, true)
	expandNoteAncestorsForMatches(groups)

	// Empty meetings/archived shells should be gone.
	for _, g := range groups {
		if g.Status == intent.StatusNoteMeetings || g.Status == intent.StatusNoteArchived {
			if g.DescendantCount == 0 {
				t.Fatalf("empty reserved folder %s should be pruned under filter", g.Status)
			}
		}
	}
	// Path to match expanded.
	for _, g := range groups {
		if g.Status == intent.StatusNote || g.Status == intent.Status("notes/reading") || g.Status == intent.Status("notes/reading/papers") {
			if (g.Status == intent.StatusNote || g.Status == intent.Status("notes/reading")) && !g.Expanded {
				t.Fatalf("ancestor %s should be expanded for filter match", g.Status)
			}
		}
	}
}

func TestDiscoveredEmptyNoteFoldersAppearOutsideFilters(t *testing.T) {
	folders := []intent.NoteFolder{
		{Status: intent.StatusNote, Name: "Notes", Depth: 0},
		{Status: intent.StatusNoteMeetings, Name: "Meetings", Depth: 1, Reserved: true},
		{Status: intent.StatusNoteArchived, Name: "Archived", Depth: 1, Reserved: true},
		{Status: intent.Status("notes/reading"), Name: "Reading", Depth: 1},
	}

	combined := groupExplorerItemsByStatus(nil, folders, false, map[string]bool{"notes": true}, false)
	notesOnly := groupNotes(nil, folders, map[string]bool{"notes": true}, false)
	for name, groups := range map[string][]IntentGroup{"combined": combined, "notes": notesOnly} {
		found := false
		for _, group := range groups {
			if group.Status == intent.Status("notes/reading") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s view omitted discovered empty folder notes/reading", name)
		}
	}

	filtered := groupExplorerItemsByStatus(nil, folders, false, map[string]bool{"notes": true}, true)
	for _, group := range filtered {
		if group.Status == intent.Status("notes/reading") {
			t.Fatal("filtered view should omit empty folder notes/reading")
		}
	}
}
