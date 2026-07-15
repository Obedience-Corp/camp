package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	dungeonscaffold "github.com/Obedience-Corp/camp/internal/dungeon/scaffold"
	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
)

// resolveStatusRoot translates a schema status path (e.g. "dungeon/completed",
// "active", ".") into its actual on-disk directory under root. A leading
// "dungeon" segment is rewritten to whichever of "dungeon"/".dungeon" is
// already established under root, or to campaignSpelling when neither exists
// yet. Non-dungeon statuses are returned unchanged.
func resolveStatusRoot(ctx context.Context, root, status, campaignSpelling string) (string, error) {
	if status != spelling.Visible && !strings.HasPrefix(status, spelling.Visible+"/") {
		return filepath.Join(root, status), nil
	}
	name, err := spelling.NameForNew(ctx, root, campaignSpelling)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, spelling.RewriteRel(status, name)), nil
}

func resolveWorkflowDestinationPath(ctx context.Context, root, status, itemName, campaignSpelling string, now time.Time) (string, error) {
	statusRoot, err := resolveStatusRoot(ctx, root, status, campaignSpelling)
	if err != nil {
		return "", err
	}
	if isStandardDungeonPath(status) {
		return statuspath.DatedItemPath(statusRoot, itemName, now), nil
	}
	return filepath.Join(statusRoot, itemName), nil
}

func resolveWorkflowItemPath(ctx context.Context, root, status, itemName, campaignSpelling string) (string, bool, error) {
	statusRoot, err := resolveStatusRoot(ctx, root, status, campaignSpelling)
	if err != nil {
		return "", false, err
	}
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
	return strings.HasPrefix(path, spelling.Visible+"/")
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
