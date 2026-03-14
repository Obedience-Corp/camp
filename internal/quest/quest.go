package quest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Obedience-Corp/camp/internal/editor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"gopkg.in/yaml.v3"
)

var (
	ErrNotInitialized       = camperrors.Wrap(camperrors.ErrNotFound, "quest system not initialized")
	ErrQuestNotFound        = camperrors.Wrap(camperrors.ErrNotFound, "quest not found")
	ErrQuestAmbiguous       = camperrors.Wrap(camperrors.ErrInvalidInput, "quest identifier is ambiguous")
	ErrDefaultQuestReadOnly = camperrors.Wrap(camperrors.ErrInvalidInput, "default quest cannot change lifecycle state")
	ErrInvalidTransition    = camperrors.Wrap(camperrors.ErrInvalidInput, "invalid quest status transition")
	ErrInvalidQuest         = camperrors.Wrap(camperrors.ErrInvalidInput, "invalid quest")
	ErrMissingID            = camperrors.Wrap(camperrors.ErrInvalidInput, "quest id is required")
	ErrMissingName          = camperrors.Wrap(camperrors.ErrInvalidInput, "quest name is required")
	ErrInvalidStatus        = camperrors.Wrap(camperrors.ErrInvalidInput, "quest status is invalid")
	ErrMissingCreatedAt     = camperrors.Wrap(camperrors.ErrInvalidInput, "quest created_at is required")
	ErrMissingUpdatedAt     = camperrors.Wrap(camperrors.ErrInvalidInput, "quest updated_at is required")
)

// QuestsDir returns the quest root directory for a campaign.
func QuestsDir(campaignRoot string) string {
	return filepath.Join(campaignRoot, RootDirName)
}

// DungeonDir returns the root quest dungeon path.
func DungeonDir(campaignRoot string) string {
	return filepath.Join(QuestsDir(campaignRoot), "dungeon")
}

// DungeonStatusDir returns the quest dungeon bucket for the given status.
func DungeonStatusDir(campaignRoot string, status Status) string {
	return filepath.Join(DungeonDir(campaignRoot), status.String())
}

// DefaultQuestPath returns the default quest metadata path.
func DefaultQuestPath(campaignRoot string) string {
	return filepath.Join(QuestsDir(campaignRoot), DefaultFileName)
}

// QuestDir returns the directory path for a quest slug at the root.
func QuestDir(campaignRoot, slug string) string {
	return filepath.Join(QuestsDir(campaignRoot), slug)
}

// QuestPathForDir returns the metadata file path for a quest directory.
func QuestPathForDir(dir string) string {
	return filepath.Join(dir, FileName)
}

// Exists reports whether the quest root exists.
func Exists(campaignRoot string) bool {
	info, err := os.Stat(QuestsDir(campaignRoot))
	return err == nil && info.IsDir()
}

// IsInitialized reports whether the canonical default quest exists.
func IsInitialized(campaignRoot string) bool {
	info, err := os.Stat(DefaultQuestPath(campaignRoot))
	return err == nil && !info.IsDir()
}

// Save writes quest metadata to disk.
func Save(ctx context.Context, path string, q *Quest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeQuestFile(path, q)
}

// Load reads quest metadata from disk.
func Load(ctx context.Context, path string) (*Quest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrQuestNotFound
		}
		return nil, camperrors.Wrapf(err, "read quest file %s", path)
	}

	var q Quest
	if err := yaml.Unmarshal(data, &q); err != nil {
		return nil, camperrors.Wrapf(err, "parse quest file %s", path)
	}
	q.Path = path
	if filepath.Base(path) == DefaultFileName {
		q.Slug = DefaultQuestName
	} else {
		q.Slug = filepath.Base(filepath.Dir(path))
	}
	if err := q.Validate(); err != nil {
		return nil, err
	}
	return &q, nil
}

// LoadDefault loads the canonical default quest.
func LoadDefault(ctx context.Context, campaignRoot string) (*Quest, error) {
	return Load(ctx, DefaultQuestPath(campaignRoot))
}

// List returns quests from the root and optionally the dungeon.
func List(ctx context.Context, campaignRoot string, includeDungeon bool) ([]*Quest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !Exists(campaignRoot) {
		return nil, ErrNotInitialized
	}

	var quests []*Quest
	if q, err := LoadDefault(ctx, campaignRoot); err == nil {
		quests = append(quests, q)
	} else if !errors.Is(err, ErrQuestNotFound) {
		return nil, err
	}

	rootEntries, err := os.ReadDir(QuestsDir(campaignRoot))
	if err != nil {
		return nil, camperrors.Wrap(err, "read quests dir")
	}
	for _, entry := range rootEntries {
		if !entry.IsDir() || entry.Name() == "dungeon" {
			continue
		}
		q, err := Load(ctx, QuestPathForDir(filepath.Join(QuestsDir(campaignRoot), entry.Name())))
		if err != nil {
			return nil, err
		}
		quests = append(quests, q)
	}

	if includeDungeon {
		for _, status := range []Status{StatusCompleted, StatusArchived} {
			statusDir := DungeonStatusDir(campaignRoot, status)
			entries, err := os.ReadDir(statusDir)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, camperrors.Wrapf(err, "read quest dungeon %s", statusDir)
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				q, err := Load(ctx, QuestPathForDir(filepath.Join(statusDir, entry.Name())))
				if err != nil {
					return nil, err
				}
				quests = append(quests, q)
			}
		}
	}

	return quests, nil
}

// Resolve finds a quest by ID, slug, or exact human name.
func Resolve(ctx context.Context, campaignRoot, identifier string) (*Quest, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, ErrQuestNotFound
	}

	quests, err := List(ctx, campaignRoot, true)
	if err != nil {
		return nil, err
	}
	for _, q := range quests {
		if q.ID == identifier {
			return q, nil
		}
	}
	for _, q := range quests {
		if q.Slug == identifier {
			return q, nil
		}
	}

	var nameMatches []*Quest
	for _, q := range quests {
		if strings.EqualFold(q.Name, identifier) {
			nameMatches = append(nameMatches, q)
		}
	}

	switch len(nameMatches) {
	case 0:
		return nil, ErrQuestNotFound
	case 1:
		return nameMatches[0], nil
	default:
		return nil, camperrors.Wrapf(ErrQuestAmbiguous, "%q matches %d quests", identifier, len(nameMatches))
	}
}

// ResolveContext resolves quest context from an explicit flag or the CAMP_QUEST
// environment variable. Returns nil (no quest context) when neither is set.
// Multiple quests may be open simultaneously; there is no "active" marker file.
func ResolveContext(ctx context.Context, campaignRoot, explicit string) (*Quest, error) {
	if strings.TrimSpace(explicit) != "" {
		return Resolve(ctx, campaignRoot, explicit)
	}

	if envID := strings.TrimSpace(os.Getenv("CAMP_QUEST")); envID != "" {
		return Resolve(ctx, campaignRoot, envID)
	}

	return nil, nil
}

// SortForList sorts quests for list output.
func SortForList(quests []*Quest) {
	statusRank := map[Status]int{
		StatusOpen:      0,
		StatusPaused:    1,
		StatusCompleted: 2,
		StatusArchived:  3,
	}

	slices.SortFunc(quests, func(a, b *Quest) int {
		if a == nil && b == nil {
			return 0
		}
		if a == nil {
			return 1
		}
		if b == nil {
			return -1
		}
		if ra, rb := statusRank[a.Status], statusRank[b.Status]; ra != rb {
			return ra - rb
		}
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			if a.UpdatedAt.After(b.UpdatedAt) {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
}

func writeQuestFile(path string, q *Quest) error {
	if q == nil {
		return ErrInvalidQuest
	}
	if err := q.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return camperrors.Wrap(err, "create quest directory")
	}

	clone := *q
	clone.Path = ""
	clone.Slug = ""
	data, err := yaml.Marshal(&clone)
	if err != nil {
		return camperrors.Wrapf(err, "marshal quest %q", q.Name)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return camperrors.Wrapf(err, "write quest file %s", path)
	}
	q.Path = path
	if filepath.Base(path) == DefaultFileName {
		q.Slug = DefaultQuestName
	} else {
		q.Slug = filepath.Base(filepath.Dir(path))
	}
	return nil
}

// RelativePath returns a campaign-root-relative path when possible.
func RelativePath(campaignRoot, path string) string {
	rel, err := filepath.Rel(campaignRoot, path)
	if err != nil {
		return path
	}
	return rel
}

// OpenInEditor opens the path in the configured user editor.
func OpenInEditor(ctx context.Context, path string) error {
	return editor.Edit(ctx, path)
}
