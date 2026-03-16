package workitem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	"gopkg.in/yaml.v3"
)

// festYAML is a minimal parse target for fest.yaml.
// Only the fields needed for work item discovery are modeled.
type festYAML struct {
	Version  string       `yaml:"version"`
	Metadata festMetadata `yaml:"metadata"`
}

type festMetadata struct {
	ID           string    `yaml:"id"`
	Name         string    `yaml:"name"`
	FestivalType string    `yaml:"festival_type"`
	CreatedAt    time.Time `yaml:"created_at"`
}

func discoverFestivals(ctx context.Context, campaignRoot string, resolver *paths.Resolver) ([]WorkItem, error) {
	festivalsRoot := resolver.Festivals()
	var items []WorkItem

	for _, stage := range []string{"planning", "ready", "active"} {
		stageDir := filepath.Join(festivalsRoot, stage)
		entries, err := os.ReadDir(stageDir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, camperrors.Wrapf(err, "reading festival stage %s", stage)
		}

		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() || strings.HasPrefix(name, ".") {
				continue
			}

			dirPath := filepath.Join(stageDir, name)
			relPath, err := filepath.Rel(campaignRoot, dirPath)
			if err != nil {
				continue // skip items with unresolvable relative paths
			}

			var meta festMetadata
			festPath := filepath.Join(dirPath, "fest.yaml")
			if data, err := os.ReadFile(festPath); err == nil {
				var fy festYAML
				if err := yaml.Unmarshal(data, &fy); err == nil {
					meta = fy.Metadata
				}
			}

			title := meta.Name
			if title == "" {
				title = humanizeBasename(name)
			}
			if meta.ID != "" {
				title = title + " (" + meta.ID + ")"
			}

			primaryDocAbs := findFestivalPrimaryDoc(dirPath)
			scanEarliest, scanLatest := ScanDirTimestamps(ctx, dirPath)
			created := meta.CreatedAt
			if created.IsZero() {
				created = scanEarliest
			}
			updated := scanLatest

			var primaryDocRel string
			if primaryDocAbs != "" {
				primaryDocRel, _ = filepath.Rel(campaignRoot, primaryDocAbs)
			}

			item := WorkItem{
				Key:            "festival:" + relPath,
				WorkflowType:   WorkflowTypeFestival,
				LifecycleStage: stage,
				Title:          title,
				RelativePath:   relPath,
				PrimaryDoc:     primaryDocRel,
				ItemKind:       ItemKindDirectory,
				CreatedAt:      created,
				UpdatedAt:      updated,
				SourceID:       meta.ID,
				SourceMetadata: map[string]any{
					"festival_type": meta.FestivalType,
				},
			}
			item.SortTimestamp = DeriveSortTimestamp(item.UpdatedAt, item.CreatedAt)
			if primaryDocAbs != "" {
				item.Summary = extractSummaryFromFile(primaryDocAbs, 200)
			}
			items = append(items, item)
		}
	}
	return items, nil
}

// findFestivalPrimaryDoc returns the best doc file for a festival directory.
func findFestivalPrimaryDoc(dir string) string {
	for _, name := range []string{"FESTIVAL_GOAL.md", "FESTIVAL_OVERVIEW.md", "fest.yaml"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
