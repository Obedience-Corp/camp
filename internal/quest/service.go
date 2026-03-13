package quest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/editor"
)

// MutationResult contains the resulting quest plus the paths that should be
// staged for a selective commit.
type MutationResult struct {
	Quest     *Quest
	Files     []string
	PreStaged []string
}

type CreateOptions struct {
	Name        string
	Purpose     string
	Description string
	Tags        []string
}

type ListOptions struct {
	Statuses []Status
	All      bool
	Dungeon  bool
}

// EditorFunc opens an editor on the provided path.
type EditorFunc func(ctx context.Context, path string) error

// Service manages quest lifecycle operations for a campaign.
type Service struct {
	campaignRoot string
}

func NewService(campaignRoot string) *Service {
	return &Service{campaignRoot: campaignRoot}
}

func (s *Service) CreateDirect(ctx context.Context, opts CreateOptions) (*Quest, error) {
	result, err := s.Create(ctx, opts.Name, opts.Purpose, opts.Description, opts.Tags)
	if err != nil {
		return nil, err
	}
	return result.Quest, nil
}

func (s *Service) CreateWithEditorOptions(ctx context.Context, opts CreateOptions, editorFn EditorFunc) (*Quest, error) {
	result, err := s.CreateWithEditor(ctx, opts.Name, opts.Purpose, opts.Description, opts.Tags, editorFn)
	if err != nil {
		return nil, err
	}
	return result.Quest, nil
}

func (s *Service) Create(ctx context.Context, name, purpose, description string, tags []string) (*MutationResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("quest name is required")
	}

	now := time.Now().UTC()
	id, err := GenerateID(now)
	if err != nil {
		return nil, err
	}
	dir, err := s.uniqueQuestDir(ctx, name, now)
	if err != nil {
		return nil, err
	}
	q := &Quest{
		ID:          id,
		Name:        name,
		Purpose:     strings.TrimSpace(purpose),
		Description: strings.TrimSpace(description),
		Status:      StatusOpen,
		CreatedAt:   now,
		UpdatedAt:   now,
		Tags:        normalizeTags(tags),
	}
	path := QuestPathForDir(dir)
	if err := Save(ctx, path, q); err != nil {
		return nil, err
	}
	if err := WriteActiveID(ctx, s.campaignRoot, q.ID); err != nil {
		return nil, err
	}

	return &MutationResult{
		Quest: q,
		Files: []string{path, ActiveQuestPath(s.campaignRoot)},
	}, nil
}

// Find resolves a quest by ID, slug, or exact name.
func (s *Service) Find(ctx context.Context, identifier string) (*Quest, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}
	return Resolve(ctx, s.campaignRoot, identifier)
}

// ReadRaw returns the raw YAML content and resolved quest metadata.
func (s *Service) ReadRaw(ctx context.Context, identifier string) ([]byte, *Quest, error) {
	q, err := s.Find(ctx, identifier)
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(q.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("read quest %s: %w", q.Path, err)
	}
	return data, q, nil
}

// List returns quests filtered by the given options.
func (s *Service) List(ctx context.Context, opts *ListOptions) ([]*Quest, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	quests, err := List(ctx, s.campaignRoot, true)
	if err != nil {
		return nil, err
	}

	filtered := make([]*Quest, 0, len(quests))
	for _, q := range quests {
		if opts == nil {
			if q.Status.InDungeon() {
				continue
			}
			filtered = append(filtered, q)
			continue
		}
		if opts.Dungeon && !q.Status.InDungeon() {
			continue
		}
		if len(opts.Statuses) == 0 && !opts.All && !opts.Dungeon && q.Status.InDungeon() {
			continue
		}
		if len(opts.Statuses) > 0 && !containsStatus(opts.Statuses, q.Status) {
			continue
		}
		filtered = append(filtered, q)
	}

	SortForList(filtered)
	return filtered, nil
}

func (s *Service) CreateWithEditor(ctx context.Context, name, purpose, description string, tags []string, editorFn EditorFunc) (*MutationResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("quest name is required")
	}

	now := time.Now().UTC()
	id, err := GenerateID(now)
	if err != nil {
		return nil, err
	}
	template := &Quest{
		ID:          id,
		Name:        name,
		Purpose:     strings.TrimSpace(purpose),
		Description: strings.TrimSpace(description),
		Status:      StatusOpen,
		CreatedAt:   now,
		UpdatedAt:   now,
		Tags:        normalizeTags(tags),
	}

	tmp, err := os.CreateTemp("", "quest_*.yaml")
	if err != nil {
		return nil, fmt.Errorf("create temp quest file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := writeQuestFile(tmpPath, template); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temp quest file: %w", err)
	}

	if editorFn == nil {
		editorFn = editor.Edit
	}
	if err := editorFn(ctx, tmpPath); err != nil {
		return nil, err
	}

	edited, err := Load(ctx, tmpPath)
	if err != nil {
		return nil, err
	}
	edited.ID = template.ID
	edited.Status = StatusOpen
	edited.CreatedAt = template.CreatedAt
	edited.UpdatedAt = time.Now().UTC()
	edited.Tags = normalizeTags(edited.Tags)

	dir, err := s.uniqueQuestDir(ctx, edited.Name, edited.CreatedAt)
	if err != nil {
		return nil, err
	}
	path := QuestPathForDir(dir)
	if err := Save(ctx, path, edited); err != nil {
		return nil, err
	}
	if err := WriteActiveID(ctx, s.campaignRoot, edited.ID); err != nil {
		return nil, err
	}

	return &MutationResult{
		Quest: edited,
		Files: []string{path, ActiveQuestPath(s.campaignRoot)},
	}, nil
}

func (s *Service) Edit(ctx context.Context, identifier string, editorFn EditorFunc) (*MutationResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	q, err := Resolve(ctx, s.campaignRoot, identifier)
	if err != nil {
		return nil, err
	}
	if q.IsDefault() {
		return nil, ErrDefaultQuestReadOnly
	}
	originalPath := q.Path

	tmp, err := os.CreateTemp("", "quest_edit_*.yaml")
	if err != nil {
		return nil, fmt.Errorf("create temp quest file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := writeQuestFile(tmpPath, q.Clone()); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temp quest file: %w", err)
	}

	if editorFn == nil {
		editorFn = editor.Edit
	}
	if err := editorFn(ctx, tmpPath); err != nil {
		return nil, err
	}

	edited, err := Load(ctx, tmpPath)
	if err != nil {
		return nil, err
	}
	edited.ID = q.ID
	edited.Status = q.Status
	edited.CreatedAt = q.CreatedAt
	edited.UpdatedAt = time.Now().UTC()
	edited.Tags = normalizeTags(edited.Tags)
	if err := Save(ctx, originalPath, edited); err != nil {
		return nil, err
	}

	return &MutationResult{
		Quest: edited,
		Files: []string{originalPath},
	}, nil
}

func (s *Service) Rename(ctx context.Context, identifier, newName string) (*MutationResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return nil, errors.New("new quest name is required")
	}

	q, err := Resolve(ctx, s.campaignRoot, identifier)
	if err != nil {
		return nil, err
	}
	if q.IsDefault() {
		return nil, ErrDefaultQuestReadOnly
	}

	q.Name = newName
	q.UpdatedAt = time.Now().UTC()
	if err := Save(ctx, q.Path, q); err != nil {
		return nil, err
	}

	return &MutationResult{
		Quest: q,
		Files: []string{q.Path},
	}, nil
}

func (s *Service) Pause(ctx context.Context, identifier string) (*MutationResult, error) {
	return s.updateInPlace(ctx, identifier, StatusOpen, StatusPaused)
}

func (s *Service) Resume(ctx context.Context, identifier string) (*MutationResult, error) {
	return s.updateInPlace(ctx, identifier, StatusPaused, StatusOpen)
}

func (s *Service) Complete(ctx context.Context, identifier string) (*MutationResult, error) {
	return s.moveToStatus(ctx, identifier, []Status{StatusOpen, StatusPaused}, StatusCompleted)
}

func (s *Service) Archive(ctx context.Context, identifier string) (*MutationResult, error) {
	return s.moveToStatus(ctx, identifier, []Status{StatusOpen, StatusPaused, StatusCompleted}, StatusArchived)
}

func (s *Service) Restore(ctx context.Context, identifier string) (*MutationResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	q, err := Resolve(ctx, s.campaignRoot, identifier)
	if err != nil {
		return nil, err
	}
	if q.IsDefault() {
		return nil, ErrDefaultQuestReadOnly
	}
	if q.Status != StatusCompleted && q.Status != StatusArchived {
		return nil, fmt.Errorf("%w: cannot restore quest from %s", ErrInvalidTransition, q.Status)
	}

	oldDir := filepath.Dir(q.Path)
	newDir := QuestDir(s.campaignRoot, q.Slug)
	if _, err := os.Stat(newDir); err == nil {
		return nil, fmt.Errorf("quest directory already exists: %s", newDir)
	}
	if err := os.MkdirAll(QuestsDir(s.campaignRoot), 0755); err != nil {
		return nil, fmt.Errorf("create quests dir: %w", err)
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return nil, fmt.Errorf("restore quest directory: %w", err)
	}

	q.Status = StatusOpen
	q.UpdatedAt = time.Now().UTC()
	q.Path = QuestPathForDir(newDir)
	if err := Save(ctx, q.Path, q); err != nil {
		return nil, err
	}
	if err := WriteActiveID(ctx, s.campaignRoot, q.ID); err != nil {
		return nil, err
	}

	return &MutationResult{
		Quest:     q,
		Files:     []string{newDir, ActiveQuestPath(s.campaignRoot)},
		PreStaged: []string{oldDir},
	}, nil
}

func (s *Service) ensureInitialized() error {
	if !Exists(s.campaignRoot) || !IsInitialized(s.campaignRoot) {
		return ErrNotInitialized
	}
	return nil
}

func (s *Service) uniqueQuestDir(ctx context.Context, name string, now time.Time) (string, error) {
	base := GenerateDirectorySlug(name, now)
	dir := QuestDir(s.campaignRoot, base)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return dir, nil
	}

	for i := 2; i < 1000; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		candidate := QuestDir(s.campaignRoot, fmt.Sprintf("%s-%d", base, i))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("could not allocate quest directory for %q", name)
}

func (s *Service) updateInPlace(ctx context.Context, identifier string, from, to Status) (*MutationResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	q, err := Resolve(ctx, s.campaignRoot, identifier)
	if err != nil {
		return nil, err
	}
	if q.IsDefault() {
		return nil, ErrDefaultQuestReadOnly
	}
	if q.Status != from {
		return nil, fmt.Errorf("%w: expected %s, found %s", ErrInvalidTransition, from, q.Status)
	}

	q.Status = to
	q.UpdatedAt = time.Now().UTC()
	if err := Save(ctx, q.Path, q); err != nil {
		return nil, err
	}

	files := []string{q.Path}
	if to == StatusPaused {
		if activeID, err := ReadActiveID(ctx, s.campaignRoot); err == nil && activeID == q.ID {
			if err := WriteActiveID(ctx, s.campaignRoot, DefaultQuestID); err != nil {
				return nil, err
			}
			files = append(files, ActiveQuestPath(s.campaignRoot))
		}
	}
	if to == StatusOpen {
		if err := WriteActiveID(ctx, s.campaignRoot, q.ID); err != nil {
			return nil, err
		}
		files = append(files, ActiveQuestPath(s.campaignRoot))
	}

	return &MutationResult{
		Quest: q,
		Files: files,
	}, nil
}

func (s *Service) moveToStatus(ctx context.Context, identifier string, from []Status, target Status) (*MutationResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	q, err := Resolve(ctx, s.campaignRoot, identifier)
	if err != nil {
		return nil, err
	}
	if q.IsDefault() {
		return nil, ErrDefaultQuestReadOnly
	}
	if !containsStatus(from, q.Status) {
		return nil, fmt.Errorf("%w: cannot move quest from %s to %s", ErrInvalidTransition, q.Status, target)
	}

	oldDir := filepath.Dir(q.Path)
	newDir := filepath.Join(DungeonStatusDir(s.campaignRoot, target), q.Slug)
	if _, err := os.Stat(newDir); err == nil {
		return nil, fmt.Errorf("quest directory already exists: %s", newDir)
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0755); err != nil {
		return nil, fmt.Errorf("create quest dungeon dir: %w", err)
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return nil, fmt.Errorf("move quest directory: %w", err)
	}

	q.Status = target
	q.UpdatedAt = time.Now().UTC()
	q.Path = QuestPathForDir(newDir)
	if err := Save(ctx, q.Path, q); err != nil {
		return nil, err
	}

	files := []string{newDir}
	if activeID, err := ReadActiveID(ctx, s.campaignRoot); err == nil && activeID == q.ID {
		if err := WriteActiveID(ctx, s.campaignRoot, DefaultQuestID); err != nil {
			return nil, err
		}
		files = append(files, ActiveQuestPath(s.campaignRoot))
	}

	return &MutationResult{
		Quest:     q,
		Files:     files,
		PreStaged: []string{oldDir},
	}, nil
}

func containsStatus(statuses []Status, target Status) bool {
	for _, status := range statuses {
		if status == target {
			return true
		}
	}
	return false
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	var out []string
	for _, tag := range tags {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}
