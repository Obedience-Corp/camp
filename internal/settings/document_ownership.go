package settings

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/quest/workspace"
)

// isIntentDocument reports an owner intent or note record. Status directories
// come from intent.AllStatuses, including the hidden dungeon spelling. The
// notes tree holds note records at any depth. Markdown files at the intents
// root and in other folders are authored text.
func isIntentDocument(rel string) bool {
	rel = strings.TrimPrefix(rel, "/")
	if formatFromName(rel) != DocumentMarkdown {
		return false
	}
	rest, ok := strings.CutPrefix(rel, "intents/")
	if !ok || !strings.Contains(rest, "/") {
		return false
	}
	dir := path.Dir(rest)
	if dir == string(intent.StatusNote) || strings.HasPrefix(dir, string(intent.StatusNote)+"/") {
		return true
	}
	return isIntentStatusDir(dir)
}

func isIntentStatusDir(dir string) bool {
	for _, status := range intent.AllStatuses() {
		bucket := string(status)
		if dir == bucket {
			return true
		}
		hidden := filepath.ToSlash(spelling.RewriteRel(bucket, spelling.Hidden))
		if hidden != bucket && dir == hidden {
			return true
		}
	}
	return false
}

// isQuestDocument reports a quest metadata file inside the quests tree.
func isQuestDocument(rel string) bool {
	return questLeaf(rel, quest.FileName)
}

// isQuestWorkspaceState reports owner runtime state inside the quests tree.
func isQuestWorkspaceState(rel string) bool {
	return questLeaf(rel, workspace.FileName)
}

func questLeaf(rel, name string) bool {
	rel = strings.TrimPrefix(rel, "/")
	rest, ok := strings.CutPrefix(rel, "quests/")
	if !ok || rest == "" {
		return false
	}
	return path.Base(rest) == name
}
