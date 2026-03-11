package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Transition represents a status transition for an item.
type Transition struct {
	Item string // Item name
	From string // Source status
	To   string // Destination status
}

// CommitMessage returns a formatted commit message for this transition.
func (t Transition) CommitMessage() string {
	return fmt.Sprintf("flow: move %s from %s to %s", t.Item, t.From, t.To)
}

// DetectTransition determines the source and destination status of a move operation.
func DetectTransition(ctx context.Context, workflowRoot, item, destination string) (Transition, error) {
	if ctx.Err() != nil {
		return Transition{}, ctx.Err()
	}

	from, err := findItemStatus(workflowRoot, item)
	if err != nil {
		return Transition{}, err
	}

	if from == destination {
		return Transition{}, fmt.Errorf("item %q is already in %q", item, destination)
	}

	return Transition{
		Item: item,
		From: from,
		To:   destination,
	}, nil
}

// findItemStatus walks the workflow directory tree to find which status contains the item.
func findItemStatus(workflowRoot, item string) (string, error) {
	entries, err := os.ReadDir(workflowRoot)
	if err != nil {
		return "", camperrors.Wrap(err, "read workflow root")
	}

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		status := entry.Name()
		itemPath := filepath.Join(workflowRoot, status, item)
		if _, err := os.Stat(itemPath); err == nil {
			return status, nil
		}

		// Check nested statuses (e.g., dungeon/completed)
		nestedEntries, err := os.ReadDir(filepath.Join(workflowRoot, status))
		if err != nil {
			continue
		}
		for _, nested := range nestedEntries {
			if !nested.IsDir() || strings.HasPrefix(nested.Name(), ".") {
				continue
			}
			nestedStatus := status + "/" + nested.Name()
			nestedPath, found, err := resolveWorkflowItemPath(workflowRoot, nestedStatus, item)
			if err != nil {
				return "", camperrors.Wrap(err, "read nested workflow status")
			}
			if found && nestedPath != "" {
				return nestedStatus, nil
			}
		}
	}

	return "", fmt.Errorf("item %q not found in any status directory", item)
}
