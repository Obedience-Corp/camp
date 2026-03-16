package workitem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/paths"
)

func discoverIntents(ctx context.Context, campaignRoot string, resolver *paths.Resolver) ([]WorkItem, error) {
	intentsRoot := resolver.Intents()
	var items []WorkItem

	for _, stage := range []string{"inbox", "active", "ready"} {
		stageDir := filepath.Join(intentsRoot, stage)
		entries, err := os.ReadDir(stageDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, camperrors.Wrapf(err, "reading intent stage %s", stage)
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			if entry.Name() == ".gitkeep" {
				continue
			}

			filePath := filepath.Join(stageDir, entry.Name())
			content, err := os.ReadFile(filePath)
			if err != nil {
				continue // skip unreadable files
			}

			i, err := intent.ParseIntentFromFile(filePath, content)
			if err != nil {
				continue // skip malformed intent files — dev tool, not worth erroring
			}

			relPath, err := filepath.Rel(campaignRoot, filePath)
			if err != nil {
				continue // skip items with unresolvable relative paths
			}
			item := WorkItem{
				Key:            "intent:" + relPath,
				WorkflowType:   WorkflowTypeIntent,
				LifecycleStage: stage,
				Title:          i.Title,
				RelativePath:   relPath,
				PrimaryDoc:     relPath, // intent file IS its own primary doc
				ItemKind:       ItemKindFile,
				CreatedAt:      i.CreatedAt,
				UpdatedAt:      i.UpdatedAt,
				SourceID:       i.ID,
				SourceMetadata: map[string]any{
					"intent_type": string(i.Type),
					"concept":     i.Concept,
					"priority":    string(i.Priority),
				},
			}
			item.SortTimestamp = DeriveSortTimestamp(item.UpdatedAt, item.CreatedAt)
			item.Summary = extractSummary(i.Content, 200)
			items = append(items, item)
		}
	}
	return items, nil
}
