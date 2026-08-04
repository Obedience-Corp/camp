package explorer

import (
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/intent"
)

// maxNoteTreeDepth is the deepest indent level rendered as nested rows.
// Deeper folders flatten their display name to the remaining path.
const maxNoteTreeDepth = 3

// buildNotesTreeGroups builds nestable IntentGroups for the notes store from
// the given notes and optional discovered folders. When folders is empty, a
// minimal tree is synthesized from note Status paths so nested notes still
// appear. foldState maps status string → Expanded; missing keys default to
// collapsed (false). When omitEmpty is true, folders with zero descendants
// are dropped (filter views must not show empty shells).
func buildNotesTreeGroups(notes []*intent.Intent, folders []intent.NoteFolder, foldState map[string]bool, omitEmpty bool) []IntentGroup {
	if len(folders) == 0 {
		folders = synthesizeFoldersFromNotes(notes)
	}
	if len(folders) == 0 {
		// Always expose the notes root so the explorer has a Notes header.
		folders = []intent.NoteFolder{{
			Status: intent.StatusNote,
			Name:   "Notes",
			Depth:  0,
		}}
	}

	groups := make([]IntentGroup, 0, len(folders))
	statusIdx := make(map[intent.Status]int, len(folders))

	for _, f := range folders {
		name := f.Name
		depth := f.Depth
		if depth > maxNoteTreeDepth {
			// Flatten label for deep folders; keep real Depth for parent wiring
			// but render clamp is applied via display depth.
			name = flattenedNoteName(f.Status)
		}
		exp := false
		if foldState != nil {
			if v, ok := foldState[string(f.Status)]; ok {
				exp = v
			}
		}
		statusIdx[f.Status] = len(groups)
		groups = append(groups, IntentGroup{
			Name:     name,
			Status:   f.Status,
			Expanded: exp,
			Depth:    depth,
		})
	}

	// Distribute notes into their folder groups. Nested statuses that somehow
	// miss the folder list land on the notes root so nothing is dropped.
	rootIdx := statusIdx[intent.StatusNote]
	for _, n := range notes {
		if !n.Status.IsNote() {
			continue
		}
		if idx, ok := statusIdx[n.Status]; ok {
			groups[idx].Intents = append(groups[idx].Intents, n)
			continue
		}
		groups[rootIdx].Intents = append(groups[rootIdx].Intents, n)
	}

	// Wire Children from path hierarchy (parent Status is the path prefix).
	for i, g := range groups {
		if g.Depth == 0 {
			continue
		}
		parent := parentNoteStatus(g.Status)
		pi, ok := statusIdx[parent]
		if !ok {
			continue
		}
		groups[pi].Children = append(groups[pi].Children, i)
	}

	// Descendant counts bottom-up (folders list is root-to-leaf DFS order).
	for i := len(groups) - 1; i >= 0; i-- {
		count := len(groups[i].Intents)
		for _, ci := range groups[i].Children {
			count += groups[ci].DescendantCount
		}
		groups[i].DescendantCount = count
	}

	if omitEmpty {
		groups = pruneEmptyNoteGroups(groups)
	}
	return groups
}

// synthesizeFoldersFromNotes builds a NoteFolder list covering every status
// path present in notes, plus structural reserved folders when any note exists.
func synthesizeFoldersFromNotes(notes []*intent.Intent) []intent.NoteFolder {
	seen := map[intent.Status]bool{intent.StatusNote: true}
	// Always include reserved so Meetings/Archived headers exist when notes load.
	seen[intent.StatusNoteMeetings] = true
	seen[intent.StatusNoteArchived] = true

	for _, n := range notes {
		if !n.Status.IsNote() {
			continue
		}
		for _, st := range statusAncestry(n.Status) {
			seen[st] = true
		}
	}

	// Build ordered list: root, reserved, then remaining sorted by path.
	var user []intent.Status
	for st := range seen {
		if st == intent.StatusNote || st == intent.StatusNoteMeetings || st == intent.StatusNoteArchived {
			continue
		}
		user = append(user, st)
	}
	// Simple string sort for stable alpha DFS-ish order.
	sortStatuses(user)

	out := []intent.NoteFolder{
		{Status: intent.StatusNote, Name: "Notes", Depth: 0},
		{Status: intent.StatusNoteMeetings, Name: "Meetings", Depth: 1, Reserved: true},
		{Status: intent.StatusNoteArchived, Name: "Archived", Depth: 1, Reserved: true},
	}
	for _, st := range user {
		out = append(out, intent.NoteFolder{
			Status: st,
			Name:   displayNameForStatus(st),
			Depth:  strings.Count(string(st), "/"),
		})
	}
	return out
}

func sortStatuses(ss []intent.Status) {
	for i := 0; i < len(ss); i++ {
		for j := i + 1; j < len(ss); j++ {
			if string(ss[j]) < string(ss[i]) {
				ss[i], ss[j] = ss[j], ss[i]
			}
		}
	}
}

func statusAncestry(st intent.Status) []intent.Status {
	parts := strings.Split(string(st), "/")
	if len(parts) == 0 {
		return nil
	}
	out := make([]intent.Status, 0, len(parts))
	for i := range parts {
		out = append(out, intent.Status(strings.Join(parts[:i+1], "/")))
	}
	return out
}

func parentNoteStatus(st intent.Status) intent.Status {
	s := string(st)
	i := strings.LastIndex(s, "/")
	if i <= 0 {
		return ""
	}
	return intent.Status(s[:i])
}

func displayNameForStatus(st intent.Status) string {
	base := filepath.Base(string(st))
	if base == "" || base == "." {
		return string(st)
	}
	// Title-case first rune for display.
	r := []rune(base)
	if len(r) == 0 {
		return base
	}
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] = r[0] - 'a' + 'A'
	}
	return string(r)
}

func flattenedNoteName(st intent.Status) string {
	s := string(st)
	const prefix = "notes/"
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}

// pruneEmptyNoteGroups drops note folder groups with zero descendants and
// rewrites Children indices. Lifecycle groups must not be passed in.
func pruneEmptyNoteGroups(groups []IntentGroup) []IntentGroup {
	keep := make([]bool, len(groups))
	for i, g := range groups {
		keep[i] = g.DescendantCount > 0 || (g.Depth == 0 && g.Status == intent.StatusNote)
		// Keep root even if empty so the Notes header remains.
		if g.Status == intent.StatusNote {
			keep[i] = true
		}
	}
	// If root has no descendants, still keep just root.
	oldToNew := make(map[int]int, len(groups))
	out := make([]IntentGroup, 0, len(groups))
	for i, g := range groups {
		if !keep[i] {
			continue
		}
		oldToNew[i] = len(out)
		out = append(out, g)
	}
	for i := range out {
		if len(out[i].Children) == 0 {
			continue
		}
		newChildren := make([]int, 0, len(out[i].Children))
		for _, ci := range out[i].Children {
			if ni, ok := oldToNew[ci]; ok {
				newChildren = append(newChildren, ni)
			}
		}
		out[i].Children = newChildren
	}
	return out
}

// expandNoteAncestorsOfMarks expands every note nest parent that is an
// ancestor of a group containing a note matching the predicate, so filtered
// nested matches become visible.
func expandNoteAncestorsForMatches(groups []IntentGroup) {
	// Collect statuses that have direct matching intents.
	hasMatch := make(map[intent.Status]bool)
	for _, g := range groups {
		if len(g.Intents) > 0 && g.Status.IsNote() {
			hasMatch[g.Status] = true
		}
	}
	if len(hasMatch) == 0 {
		return
	}
	// Expand all ancestors of matching statuses.
	statusIdx := make(map[intent.Status]int, len(groups))
	for i, g := range groups {
		if g.Status.IsNote() || g.Status == intent.StatusNote {
			statusIdx[g.Status] = i
		}
	}
	for st := range hasMatch {
		for _, anc := range statusAncestry(st) {
			if idx, ok := statusIdx[anc]; ok {
				groups[idx].Expanded = true
			}
		}
	}
}
