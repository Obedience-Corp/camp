package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	dungeonscaffold "github.com/Obedience-Corp/camp/internal/dungeon/scaffold"
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

func isStandardDungeonPath(path string) bool {
	return strings.HasPrefix(path, "dungeon/")
}

func appendStandardDungeonInitResult(result *InitResult, root string, dungeonResult *dungeonscaffold.InitResult) {
	for _, dir := range dungeonResult.CreatedDirs {
		result.CreatedDirs = append(result.CreatedDirs, relativeWorkflowPath(root, dir))
	}
	for _, file := range dungeonResult.CreatedFiles {
		result.CreatedFiles = append(result.CreatedFiles, relativeWorkflowPath(root, file))
	}
	for _, skipped := range dungeonResult.Skipped {
		result.Skipped = append(result.Skipped, relativeWorkflowPath(root, skipped))
	}
}

func appendStandardDungeonMigrationResult(result *MigrateResult, root string, dungeonResult *dungeonscaffold.InitResult) {
	for _, dir := range dungeonResult.CreatedDirs {
		result.Created = append(result.Created, relativeWorkflowPath(root, dir))
	}
	for _, file := range dungeonResult.CreatedFiles {
		result.Created = append(result.Created, relativeWorkflowPath(root, file))
	}
}

func relativeWorkflowPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}
