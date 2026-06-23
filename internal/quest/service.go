package quest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/editor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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

var (
	nowUTC          = func() time.Time { return time.Now().UTC() }
	generateQuestID = GenerateID
)

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

	now := nowUTC()
	id, err := s.generateUniqueID(ctx, now)
	if err != nil {
		return nil, err
	}
	dir, err := s.claimQuestDir(ctx, name, now)
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
		cleanupClaimedQuestDir(dir)
		return nil, err
	}

	return &MutationResult{
		Quest: q,
		Files: []string{path},
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
		return nil, nil, camperrors.Wrapf(err, "read quest %s", q.Path)
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
		if len(opts.Statuses) > 0 && !slices.Contains(opts.Statuses, q.Status) {
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

	now := nowUTC()
	id, err := s.generateUniqueID(ctx, now)
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
		return nil, camperrors.Wrap(err, "create temp quest file")
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := writeQuestFile(tmpPath, template); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, camperrors.Wrap(err, "close temp quest file")
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
	edited.UpdatedAt = nowUTC()
	edited.Tags = normalizeTags(edited.Tags)

	dir, err := s.claimQuestDir(ctx, edited.Name, edited.CreatedAt)
	if err != nil {
		return nil, err
	}
	path := QuestPathForDir(dir)
	if err := Save(ctx, path, edited); err != nil {
		cleanupClaimedQuestDir(dir)
		return nil, err
	}

	return &MutationResult{
		Quest: edited,
		Files: []string{path},
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
		return nil, camperrors.Wrap(err, "create temp quest file")
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := writeQuestFile(tmpPath, q.Clone()); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, camperrors.Wrap(err, "close temp quest file")
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
		return nil, camperrors.Wrapf(ErrInvalidTransition, "cannot restore quest from %s", q.Status)
	}

	oldDir := filepath.Dir(q.Path)
	newDir := QuestDir(s.campaignRoot, q.Slug)
	if _, err := os.Stat(newDir); err == nil {
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "quest directory already exists: %s", newDir)
	}
	if err := os.MkdirAll(QuestsDir(s.campaignRoot), 0755); err != nil {
		return nil, camperrors.Wrap(err, "create quests dir")
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return nil, camperrors.Wrap(err, "restore quest directory")
	}

	q.Status = StatusOpen
	q.UpdatedAt = time.Now().UTC()
	q.Path = QuestPathForDir(newDir)
	if err := Save(ctx, q.Path, q); err != nil {
		// Rollback: move the directory back to prevent a stranded quest.
		if rbErr := os.Rename(newDir, oldDir); rbErr == nil {
			q.Path = QuestPathForDir(oldDir)
		}
		return nil, err
	}

	return &MutationResult{
		Quest:     q,
		Files:     []string{newDir},
		PreStaged: []string{oldDir},
	}, nil
}

// Link associates a campaign artifact with a quest.
// If linkType is empty, the type is auto-detected from the path.
func (s *Service) Link(ctx context.Context, identifier, path, linkType string) (*MutationResult, error) {
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

	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("link path is required")
	}

	if err := ValidateLinkPath(s.campaignRoot, path); err != nil {
		return nil, err
	}

	if linkType == "" {
		linkType = DetectLinkType(path)
	}

	link := Link{
		Path:    path,
		Type:    linkType,
		AddedAt: time.Now().UTC(),
	}
	if err := AddLink(q, link); err != nil {
		return nil, err
	}

	q.UpdatedAt = time.Now().UTC()
	if err := Save(ctx, q.Path, q); err != nil {
		return nil, err
	}

	return &MutationResult{
		Quest: q,
		Files: []string{q.Path},
	}, nil
}

// Unlink removes a linked artifact from a quest.
func (s *Service) Unlink(ctx context.Context, identifier, path string) (*MutationResult, error) {
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

	if err := RemoveLink(q, path); err != nil {
		return nil, err
	}

	q.UpdatedAt = time.Now().UTC()
	if err := Save(ctx, q.Path, q); err != nil {
		return nil, err
	}

	return &MutationResult{
		Quest: q,
		Files: []string{q.Path},
	}, nil
}

// Links returns all links associated with a quest.
func (s *Service) Links(ctx context.Context, identifier string) ([]Link, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}

	q, err := Resolve(ctx, s.campaignRoot, identifier)
	if err != nil {
		return nil, err
	}

	return q.Links, nil
}

func (s *Service) ensureInitialized() error {
	if !Exists(s.campaignRoot) || !IsInitialized(s.campaignRoot) {
		return ErrNotInitialized
	}
	return nil
}

func (s *Service) generateUniqueID(ctx context.Context, now time.Time) (string, error) {
	var collided string
	for attempt := 0; attempt < 2; attempt++ {
		id, err := generateQuestID(now)
		if err != nil {
			return "", camperrors.Wrap(err, "generate quest id")
		}
		exists, err := s.questIDExists(ctx, id)
		if err != nil {
			return "", err
		}
		if !exists {
			return id, nil
		}
		collided = id
	}
	return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "quest id collision after retry: %s", collided)
}

func (s *Service) questIDExists(ctx context.Context, id string) (bool, error) {
	quests, err := List(ctx, s.campaignRoot, true)
	if err != nil {
		return false, err
	}
	for _, q := range quests {
		if q.ID == id {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) claimQuestDir(ctx context.Context, name string, now time.Time) (string, error) {
	base := GenerateDirectorySlug(name, now)
	for i := 0; i < 1000; i++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		slug := base
		if i > 0 {
			slug = fmt.Sprintf("%s-%d", base, i+1)
		}
		dir := QuestDir(s.campaignRoot, slug)
		if err := os.Mkdir(dir, 0755); err == nil {
			return dir, nil
		} else if !os.IsExist(err) {
			return "", camperrors.Wrapf(err, "create quest dir %s", dir)
		}
	}

	return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "could not allocate quest directory for %q after 1000 attempts", name)
}

func cleanupClaimedQuestDir(dir string) {
	_ = os.Remove(dir)
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
		return nil, camperrors.Wrapf(ErrInvalidTransition, "expected %s, found %s", from, q.Status)
	}

	q.Status = to
	q.UpdatedAt = time.Now().UTC()
	if err := Save(ctx, q.Path, q); err != nil {
		return nil, err
	}

	return &MutationResult{
		Quest: q,
		Files: []string{q.Path},
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
	if !slices.Contains(from, q.Status) {
		return nil, camperrors.Wrapf(ErrInvalidTransition, "cannot move quest from %s to %s", q.Status, target)
	}

	oldDir := filepath.Dir(q.Path)
	newDir := filepath.Join(DungeonStatusDir(s.campaignRoot, target), q.Slug)
	if _, err := os.Stat(newDir); err == nil {
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "quest directory already exists: %s", newDir)
	}
	if err := os.MkdirAll(filepath.Dir(newDir), 0755); err != nil {
		return nil, camperrors.Wrap(err, "create quest dungeon dir")
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return nil, camperrors.Wrap(err, "move quest directory")
	}

	q.Status = target
	q.UpdatedAt = time.Now().UTC()
	q.Path = QuestPathForDir(newDir)
	if err := Save(ctx, q.Path, q); err != nil {
		// Rollback: move the directory back to prevent a stranded quest.
		if rbErr := os.Rename(newDir, oldDir); rbErr == nil {
			q.Path = QuestPathForDir(oldDir)
		}
		return nil, err
	}

	return &MutationResult{
		Quest:     q,
		Files:     []string{newDir},
		PreStaged: []string{oldDir},
	}, nil
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
