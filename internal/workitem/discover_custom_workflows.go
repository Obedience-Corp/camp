package workitem

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
)

// discoverCustomWorkflowTypes scans workflow/<type>/<slug>/.workitem markers
// for any non-builtin workflow type. Builtin types (intent/design/explore/
// festival) keep their dedicated discovery paths; custom types (feature, bug,
// chore, incident, ...) only surface when an explicit `.workitem` marker is
// present, matching the contract enforced by emitCandidateFS.
//
// This is the production wiring for `camp workitem create` / `camp workitem
// adopt`: those commands write a marker under workflow/<type>/<slug>/.workitem,
// and this function ensures the resulting workitem appears in `camp workitem`.
func discoverCustomWorkflowTypes(ctx context.Context, campaignRoot string, resolver *paths.Resolver) ([]WorkItem, error) {
	workflowRoot := resolver.Workflow()
	entries, err := os.ReadDir(workflowRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "reading %s", workflowRoot)
	}

	var items []WorkItem
	for _, typeEntry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		typeName := typeEntry.Name()
		if !typeEntry.IsDir() || strings.HasPrefix(typeName, ".") || typeName == "dungeon" {
			continue
		}
		if builtinTypes[WorkflowType(typeName)] {
			continue
		}

		typeDir := filepath.Join(workflowRoot, typeName)
		typeChildren, err := os.ReadDir(typeDir)
		if err != nil {
			return nil, camperrors.Wrapf(err, "reading %s", typeDir)
		}

		for _, child := range typeChildren {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			childName := child.Name()
			if !child.IsDir() || strings.HasPrefix(childName, ".") || childName == "dungeon" {
				continue
			}

			dirPath := filepath.Join(typeDir, childName)
			markerPath := filepath.Join(dirPath, MetadataFilename)
			if _, statErr := os.Stat(markerPath); statErr != nil {
				if errors.Is(statErr, os.ErrNotExist) {
					continue
				}
				slog.Default().Warn("workitem custom-type marker stat failed",
					"path", markerPath, "error", statErr.Error())
				continue
			}

			item, ok := buildWorkflowDirItem(ctx, campaignRoot, dirPath, WorkflowType(typeName))
			if !ok {
				continue
			}
			items = append(items, item)
		}
	}
	return items, nil
}
