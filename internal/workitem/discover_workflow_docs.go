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

func discoverDesign(ctx context.Context, campaignRoot string, resolver *paths.Resolver) ([]WorkItem, error) {
	return discoverWorkflowDocs(ctx, campaignRoot, resolver.Design(), WorkflowTypeDesign)
}

func discoverExplore(ctx context.Context, campaignRoot string, resolver *paths.Resolver) ([]WorkItem, error) {
	return discoverWorkflowDocs(ctx, campaignRoot, resolver.Explore(), WorkflowTypeExplore)
}

func discoverWorkflowDocs(ctx context.Context, campaignRoot, rootDir string, wfType WorkflowType) ([]WorkItem, error) {
	entries, err := os.ReadDir(rootDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "reading %s", rootDir)
	}

	var items []WorkItem
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || name == "dungeon" || strings.HasPrefix(name, ".") {
			continue
		}

		dirPath := filepath.Join(rootDir, name)
		relPath, err := filepath.Rel(campaignRoot, dirPath)
		if err != nil {
			continue // skip items with unresolvable relative paths
		}

		primaryDocAbs := findPrimaryDoc(dirPath)
		title := humanizeBasename(name)
		if primaryDocAbs != "" {
			if heading := extractFirstHeading(primaryDocAbs); heading != "" {
				title = heading
			}
		}

		created, updated := ScanDirTimestamps(ctx, dirPath)

		var primaryDocRel string
		if primaryDocAbs != "" {
			primaryDocRel, _ = filepath.Rel(campaignRoot, primaryDocAbs)
		}

		item := WorkItem{
			Key:            string(wfType) + ":" + relPath,
			WorkflowType:   wfType,
			LifecycleStage: "",
			Title:          title,
			RelativePath:   relPath,
			PrimaryDoc:     primaryDocRel,
			ItemKind:       ItemKindDirectory,
			CreatedAt:      created,
			UpdatedAt:      updated,
			SourceMetadata: map[string]any{
				"has_readme": primaryDocAbs != "" && filepath.Base(primaryDocAbs) == "README.md",
			},
		}
		item.SortTimestamp = DeriveSortTimestamp(item.UpdatedAt, item.CreatedAt)
		if primaryDocAbs != "" {
			item.Summary = extractSummaryFromFile(primaryDocAbs, 200)
		}

		md, err := LoadMetadata(ctx, dirPath)
		if err != nil {
			// Malformed optional metadata must not crash full discovery.
			// Log and include the item with derived fields only.
			slog.Default().Warn("workitem metadata invalid; skipping metadata merge",
				"path", relPath, "error", err.Error())
		} else {
			item = ApplyMetadata(item, md)
		}

		items = append(items, item)
	}
	return items, nil
}
