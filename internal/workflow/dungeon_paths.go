package workflow

import (
	"os"
	"path/filepath"
	"time"

	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
)

func resolveWorkflowDestinationPath(root, status, itemName string, now time.Time) string {
	statusRoot := filepath.Join(root, status)
	if isStandardDungeonPath(status) {
		return statuspath.DatedItemPath(statusRoot, itemName, now)
	}
	return filepath.Join(statusRoot, itemName)
}

func resolveWorkflowItemPath(root, status, itemName string) (string, bool, error) {
	statusRoot := filepath.Join(root, status)
	if isStandardDungeonPath(status) {
		return statuspath.ExistingItemPath(statusRoot, itemName)
	}

	itemPath := filepath.Join(statusRoot, itemName)
	if _, err := os.Stat(itemPath); err == nil {
		return itemPath, true, nil
	} else if os.IsNotExist(err) {
		return "", false, nil
	} else {
		return "", false, err
	}
}
