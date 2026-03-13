package quest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	dungeonscaffold "github.com/Obedience-Corp/camp/internal/dungeon/scaffold"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ScaffoldResult captures what quest scaffolding created.
type ScaffoldResult struct {
	CreatedDirs  []string
	CreatedFiles []string
	Skipped      []string
}

// RootPath returns the quest root path for a campaign.
func RootPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, RootDirName)
}

// DefaultPath returns the default quest metadata file path.
func DefaultPath(campaignRoot string) string {
	return filepath.Join(RootPath(campaignRoot), DefaultFileName)
}

// ActivePath returns the active quest marker path.
func ActivePath(campaignRoot string) string {
	return filepath.Join(RootPath(campaignRoot), ActiveFileName)
}

// DungeonPath returns the quest dungeon root.
func DungeonPath(campaignRoot string) string {
	return filepath.Join(RootPath(campaignRoot), "dungeon")
}

// EnsureScaffold creates the quest metadata root, default quest, active marker,
// and quest dungeon structure when missing.
func EnsureScaffold(ctx context.Context, campaignRoot string) (*ScaffoldResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	root := RootPath(campaignRoot)
	result := &ScaffoldResult{}

	if _, err := os.Stat(root); os.IsNotExist(err) {
		if err := os.MkdirAll(root, 0755); err != nil {
			return nil, camperrors.Wrapf(err, "creating directory %s", root)
		}
		result.CreatedDirs = append(result.CreatedDirs, root)
	}

	if err := ensureDefaultQuest(ctx, campaignRoot, result); err != nil {
		return nil, err
	}
	if err := ensureActiveMarker(ctx, campaignRoot, result); err != nil {
		return nil, err
	}

	dungeonRoot := DungeonPath(campaignRoot)
	dungeonResult, err := dungeonscaffold.Init(ctx, dungeonRoot, dungeonscaffold.InitOptions{})
	if err != nil {
		return nil, camperrors.Wrap(err, "initializing quest dungeon")
	}

	result.CreatedDirs = appendUnique(result.CreatedDirs, dungeonResult.CreatedDirs...)
	result.CreatedFiles = appendUnique(result.CreatedFiles, dungeonResult.CreatedFiles...)
	result.Skipped = appendUnique(result.Skipped, dungeonResult.Skipped...)

	return result, nil
}

// DefaultQuest returns the canonical fallback quest metadata.
func DefaultQuest(now time.Time) *Quest {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return &Quest{
		ID:          DefaultQuestID,
		Name:        DefaultQuestName,
		Purpose:     "Default working context for this campaign",
		Description: "Fallback quest for work that doesn't belong to a specific quest.",
		Status:      StatusOpen,
		CreatedAt:   now.UTC(),
		UpdatedAt:   now.UTC(),
		Slug:        DefaultQuestName,
	}
}

func ensureDefaultQuest(ctx context.Context, campaignRoot string, result *ScaffoldResult) error {
	path := DefaultPath(campaignRoot)
	if _, err := os.Stat(path); err == nil {
		result.Skipped = appendUnique(result.Skipped, path)
		return nil
	}

	q := DefaultQuest(time.Now().UTC())
	if err := Save(ctx, path, q); err != nil {
		return camperrors.Wrap(err, "writing default quest")
	}
	result.CreatedFiles = appendUnique(result.CreatedFiles, path)
	return nil
}

func ensureActiveMarker(ctx context.Context, campaignRoot string, result *ScaffoldResult) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled")
	}

	path := ActivePath(campaignRoot)
	if _, err := os.Stat(path); err == nil {
		result.Skipped = appendUnique(result.Skipped, path)
		return nil
	}

	if err := os.WriteFile(path, []byte(DefaultQuestID+"\n"), 0644); err != nil {
		return camperrors.Wrap(err, "writing active quest marker")
	}
	result.CreatedFiles = appendUnique(result.CreatedFiles, path)
	return nil
}

func appendUnique(dst []string, items ...string) []string {
	seen := make(map[string]struct{}, len(dst))
	for _, item := range dst {
		seen[item] = struct{}{}
	}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		dst = append(dst, item)
		seen[item] = struct{}{}
	}
	return dst
}
