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
		item, ok := buildWorkflowDirItem(ctx, campaignRoot, dirPath, wfType)
		if !ok {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

// buildWorkflowDirItem builds a WorkItem for one directory under workflow/<type>/.
// Returns (item, false) when the directory cannot be made campaign-relative.
// Used by both builtin discovery (design/explore) and marker-gated custom-type
// discovery so they share metadata + local-runtime merge semantics.
func buildWorkflowDirItem(ctx context.Context, campaignRoot, dirPath string, wfType WorkflowType) (WorkItem, bool) {
	relPath, err := filepath.Rel(campaignRoot, dirPath)
	if err != nil {
		return WorkItem{}, false
	}

	primaryDocAbs := findPrimaryDoc(dirPath)
	title := humanizeBasename(filepath.Base(dirPath))
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

	absDir, _ := filepath.Abs(dirPath)
	md, err := LoadMetadata(ctx, dirPath)
	if err != nil {
		slog.Default().Debug("workitem discovery skip",
			"path", absDir,
			"reason", "parse-error",
			"type", string(wfType),
			"error", err.Error())
	} else if md != nil {
		merged, applyErr := ApplyMetadata(item, md)
		if applyErr != nil {
			slog.Default().Debug("workitem discovery skip",
				"path", absDir,
				"reason", "parse-error",
				"type", string(wfType),
				"error", applyErr.Error())
		} else {
			item = merged
		}
	}

	run, runErr := LoadLocalRun(ctx, dirPath)
	if runErr != nil {
		slog.Default().Warn("workitem local runtime invalid; skipping progress",
			"path", relPath, "error", runErr.Error())
	} else if run != nil {
		if item.WorkflowMeta == nil {
			item.WorkflowMeta = &WorkItemWorkflow{}
		}
		if item.WorkflowMeta.WorkflowID == "" {
			item.WorkflowMeta.WorkflowID = run.WorkflowID
		}
		if item.WorkflowMeta.ActiveRunID == "" {
			item.WorkflowMeta.ActiveRunID = run.ActiveRunID
		}
		item.WorkflowMeta.CurrentStep = run.CurrentStep
		item.WorkflowMeta.TotalSteps = run.TotalSteps
		item.WorkflowMeta.CompletedSteps = run.CompletedSteps
		item.WorkflowMeta.RunStatus = run.RunStatus
		item.WorkflowMeta.Blocked = run.Blocked
		item.WorkflowMeta.DocHashChanged = run.DocHashChanged
	}

	return item, true
}
