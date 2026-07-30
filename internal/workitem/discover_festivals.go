package workitem

import (
	"context"
	"errors"
	"log/slog"
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

// lifecycleDirKind classifies a directory in a festivals/ stage folder. The
// .workitem marker tells festivals and residents apart: camp owns the marker,
// fest owns fest.yaml.
type lifecycleDirKind int

const (
	dirIsFestival lifecycleDirKind = iota
	dirIsResident
	// dirIsSkipped is camp's directory but not emittable as a resident: not on a
	// rail stage, or its marker would not load. Emitted as neither, since calling
	// camp's own directory a festival would be wrong. The slog reason says which.
	dirIsSkipped
)

func discoverFestivals(ctx context.Context, campaignRoot string, resolver *paths.Resolver) ([]WorkItem, error) {
	festivalsRoot := resolver.Festivals()
	var items []WorkItem

	for _, stage := range []LifecycleStage{
		LifecycleStagePlanning,
		LifecycleStageReady,
		LifecycleStageActive,
		LifecycleStageRitual,
		LifecycleStageChains,
	} {
		stageItems, err := discoverFestivalStage(ctx, campaignRoot, festivalsRoot, stage)
		if err != nil {
			return nil, err
		}
		items = append(items, stageItems...)
	}
	return items, nil
}

// discoverFestivalStage splits one lifecycle folder into residents and festivals.
func discoverFestivalStage(ctx context.Context, campaignRoot, festivalsRoot string, stage LifecycleStage) ([]WorkItem, error) {
	stageDir := filepath.Join(festivalsRoot, string(stage))
	entries, err := os.ReadDir(stageDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "reading festival stage %s", stage)
	}

	var items []WorkItem
	for _, entry := range entries {
		name := entry.Name()
		// The dot-prefix skip also keeps festivals/.dungeon out of this scan.
		if !entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}

		dirPath := filepath.Join(stageDir, name)
		resident, kind := residentFromMarker(ctx, campaignRoot, dirPath, stage)
		switch kind {
		case dirIsResident:
			items = append(items, resident)
			continue
		case dirIsSkipped:
			continue
		}

		if item, ok := festivalItem(ctx, campaignRoot, dirPath, name, stage); ok {
			items = append(items, item)
		}
	}
	return items, nil
}

// residentFromMarker classifies a lifecycle directory by its .workitem marker.
// A resident is built through buildWorkflowDirItem with its own marker's type, so
// it inherits marker and .workflow/ semantics from the workflow path; only the
// stage is overridden to the folder it sits in.
func residentFromMarker(ctx context.Context, campaignRoot, dirPath string, stage LifecycleStage) (WorkItem, lifecycleDirKind) {
	meta, err := LoadMetadata(ctx, dirPath)
	if err != nil {
		// Unreadable still proves the directory is camp's; do not emit a festival.
		slog.Default().Debug("workitem discovery skip",
			"path", dirPath, "reason", "parse-error", "error", err.Error())
		return WorkItem{}, dirIsSkipped
	}
	if meta == nil {
		return WorkItem{}, dirIsFestival
	}

	if stage != LifecycleStageReady && stage != LifecycleStageActive {
		slog.Default().Debug("workitem discovery skip",
			"path", dirPath, "reason", "resident-out-of-stage", "stage", string(stage))
		return WorkItem{}, dirIsSkipped
	}

	// Resident wins: .workitem records a promote camp performed, while a stale
	// fest.yaml proves nothing about the current owner.
	if _, statErr := os.Stat(filepath.Join(dirPath, "fest.yaml")); statErr == nil {
		slog.Default().Warn("lifecycle directory has both .workitem and fest.yaml; classifying as resident",
			"path", dirPath, "stage", string(stage), "type", meta.Type)
	}

	item, ok := buildWorkflowDirItem(ctx, campaignRoot, dirPath, WorkflowType(meta.Type))
	if !ok {
		return WorkItem{}, dirIsSkipped
	}
	item.LifecycleStage = stage
	return item, dirIsResident
}

// festivalItem builds the festival WorkItem, unchanged from pre-rail behavior
// including the marker-less humanized fallback.
func festivalItem(ctx context.Context, campaignRoot, dirPath, name string, stage LifecycleStage) (WorkItem, bool) {
	relPath, err := filepath.Rel(campaignRoot, dirPath)
	if err != nil {
		return WorkItem{}, false // skip items with unresolvable relative paths
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
		UpdatedAt:      scanLatest,
		SourceID:       meta.ID,
		SourceMetadata: map[string]any{
			"festival_type": meta.FestivalType,
		},
		Tags:     []string{},
		Projects: []string{},
	}
	item.SortTimestamp = DeriveSortTimestamp(item.UpdatedAt, item.CreatedAt)
	if primaryDocAbs != "" {
		item.Summary = extractSummaryFromFile(primaryDocAbs, 200)
	}
	return item, true
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
