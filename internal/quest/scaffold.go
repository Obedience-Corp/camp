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

// EnsureQuestDungeon creates the quest dungeon structure when missing.
// The quests directory and default quest are handled by the scaffold template
// system; this function only initialises the imperative dungeon subdirectory.
func EnsureQuestDungeon(ctx context.Context, campaignRoot string) (*ScaffoldResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	result := &ScaffoldResult{}

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

// EnsureScaffold creates the quest directory, default quest, and dungeon
// structure when missing. In production init flows the quests directory and
// default/quest.yaml are created by the scaffold template system; this function
// remains for runtime "ensure" calls (e.g. quest commands) and tests.
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

	dungeonResult, err := EnsureQuestDungeon(ctx, campaignRoot)
	if err != nil {
		return nil, err
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

	// Migrate legacy flat-file default.yaml → default/quest.yaml.
	legacyPath := LegacyDefaultPath(campaignRoot)
	if _, err := os.Stat(legacyPath); err == nil {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return camperrors.Wrapf(err, "creating default quest directory %s", dir)
			}
			if err := os.Rename(legacyPath, path); err != nil {
				return camperrors.Wrap(err, "migrating legacy default.yaml to default/quest.yaml")
			}
			result.CreatedDirs = appendUnique(result.CreatedDirs, dir)
			result.CreatedFiles = appendUnique(result.CreatedFiles, path)
			return nil
		}
		// Both exist — remove the legacy file, keep the directory version.
		os.Remove(legacyPath)
	}

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

// RootPath returns the quest root path for a campaign.
func RootPath(campaignRoot string) string {
	return QuestsDir(campaignRoot)
}

// DefaultPath returns the default quest metadata file path.
func DefaultPath(campaignRoot string) string {
	return DefaultQuestPath(campaignRoot)
}

// DungeonPath returns the quest dungeon root.
func DungeonPath(campaignRoot string) string {
	return DungeonDir(campaignRoot)
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
