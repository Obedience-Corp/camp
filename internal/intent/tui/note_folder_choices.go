package tui

import (
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/intent"
)

// NoteFolderChoices builds capture destinations from discovered note folders.
// The notes root is pinned first; user folders follow by most-recently modified.
// Reserved folders are never capture destinations.
func NoteFolderChoices(folders []intent.NoteFolder) []NoteFolderChoice {
	users := make([]intent.NoteFolder, 0, len(folders))
	for _, folder := range folders {
		if folder.Reserved || folder.Status == intent.StatusNote {
			continue
		}
		users = append(users, folder)
	}
	if len(users) == 0 {
		return nil
	}

	sort.SliceStable(users, func(i, j int) bool {
		if users[i].ModTime.Equal(users[j].ModTime) {
			return users[i].Status < users[j].Status
		}
		return users[i].ModTime.After(users[j].ModTime)
	})

	choices := make([]NoteFolderChoice, 0, len(users)+1)
	choices = append(choices, NoteFolderChoice{Label: "Notes (default)"})
	for _, folder := range users {
		rel := strings.TrimPrefix(string(folder.Status), string(intent.StatusNote)+"/")
		choices = append(choices, NoteFolderChoice{Rel: rel, Label: rel})
	}
	return choices
}
