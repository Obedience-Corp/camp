package workitem

import (
	"context"
	"errors"
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
		relPath, _ := filepath.Rel(campaignRoot, dirPath)

		primaryDoc := findPrimaryDoc(dirPath)
		title := humanizeBasename(name)
		if primaryDoc != "" {
			if heading := extractFirstHeading(primaryDoc); heading != "" {
				title = heading
			}
		}

		created, updated := ScanDirTimestamps(ctx, dirPath)

		item := WorkItem{
			Key:            string(wfType) + ":" + relPath,
			WorkflowType:   wfType,
			LifecycleStage: "",
			Title:          title,
			RelativePath:   relPath,
			AbsolutePath:   dirPath,
			PrimaryDoc:     primaryDoc,
			ItemKind:       ItemKindDirectory,
			CreatedAt:      created,
			UpdatedAt:      updated,
			SourceMetadata: map[string]any{
				"has_readme": primaryDoc != "" && filepath.Base(primaryDoc) == "README.md",
			},
		}
		item.SortTimestamp = DeriveSortTimestamp(item.UpdatedAt, item.CreatedAt)
		if primaryDoc != "" {
			item.Summary = extractSummaryFromFile(primaryDoc, 200)
		}
		items = append(items, item)
	}
	return items, nil
}
