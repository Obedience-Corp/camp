package quest

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// ChecklistMutationResult describes the outcome of a checklist change. Files
// holds the checklist path for a selective auto-commit; Item points at the
// affected item (nil for whole-list reads).
type ChecklistMutationResult struct {
	Quest     *Quest
	Checklist *Checklist
	Item      *ChecklistItem
	Files     []string
}

// AddChecklistItemOptions configures a new checklist item. Workitem is already
// resolved by the caller so internal/quest stays free of workitem coupling.
type AddChecklistItemOptions struct {
	Title    string
	Notes    string
	Status   ChecklistItemStatus
	Workitem *ChecklistWorkitem
	Rank     *int
}

// EditChecklistItemOptions carries optional field updates. Nil fields are left
// unchanged.
type EditChecklistItemOptions struct {
	Title  *string
	Notes  *string
	Status *ChecklistItemStatus
}

// Checklist loads a quest's checklist along with the resolved quest.
func (s *Service) Checklist(ctx context.Context, identifier string) (*Quest, *Checklist, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, nil, err
	}
	q, err := Resolve(ctx, s.campaignRoot, identifier)
	if err != nil {
		return nil, nil, err
	}
	cl, err := LoadChecklist(ctx, ChecklistPathForQuest(q), q.ID)
	if err != nil {
		return nil, nil, err
	}
	return q, cl, nil
}

// mutateChecklist runs a checklist mutation under an exclusive file lock so
// concurrent `camp quest item …` invocations (multi-agent / parallel sessions
// on the same quest) cannot clobber each other's writes. The lock guards the
// full load → mutate → save window, mirroring the workitem links WithLock
// transaction. fn mutates the loaded checklist in place and returns the
// affected item for the result payload.
func (s *Service) mutateChecklist(ctx context.Context, identifier string, fn func(cl *Checklist) (*ChecklistItem, error)) (*ChecklistMutationResult, error) {
	if err := s.ensureInitialized(); err != nil {
		return nil, err
	}
	q, err := Resolve(ctx, s.campaignRoot, identifier)
	if err != nil {
		return nil, err
	}
	path := ChecklistPathForQuest(q)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, camperrors.Wrap(err, "create quest directory")
	}

	release, err := fsutil.AcquireFileLock(ctx, path+".lock")
	if err != nil {
		return nil, err
	}
	defer release()

	cl, err := LoadChecklist(ctx, path, q.ID)
	if err != nil {
		return nil, err
	}
	item, err := fn(cl)
	if err != nil {
		return nil, err
	}
	if err := SaveChecklist(ctx, path, cl); err != nil {
		return nil, err
	}
	return &ChecklistMutationResult{
		Quest:     q,
		Checklist: cl,
		Item:      item,
		Files:     []string{path},
	}, nil
}

// AddChecklistItem appends a new work unit to the quest checklist.
func (s *Service) AddChecklistItem(ctx context.Context, identifier string, opts AddChecklistItemOptions) (*ChecklistMutationResult, error) {
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		return nil, ErrEmptyChecklistTitle
	}
	status := opts.Status
	if status == "" {
		status = ItemOpen
	}
	if !status.Valid() {
		return nil, ErrInvalidChecklistStatus
	}
	if opts.Rank != nil && *opts.Rank < 0 {
		return nil, ErrNegativeChecklistRank
	}

	return s.mutateChecklist(ctx, identifier, func(cl *Checklist) (*ChecklistItem, error) {
		now := nowUTC()
		id, err := GenerateChecklistItemID(now, cl.idSet())
		if err != nil {
			return nil, err
		}
		rank := cl.NextRank()
		if opts.Rank != nil {
			rank = *opts.Rank
		}

		item := ChecklistItem{
			ID:        id,
			Title:     title,
			Status:    status,
			Rank:      rank,
			Workitem:  normalizeWorkitem(opts.Workitem),
			Notes:     strings.TrimSpace(opts.Notes),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if status.Terminal() {
			completed := now
			item.CompletedAt = &completed
		}
		return cl.Add(item), nil
	})
}

// SetChecklistItemStatus transitions an item to the target status.
func (s *Service) SetChecklistItemStatus(ctx context.Context, identifier, selector string, status ChecklistItemStatus) (*ChecklistMutationResult, error) {
	if !status.Valid() {
		return nil, ErrInvalidChecklistStatus
	}
	return s.mutateChecklist(ctx, identifier, func(cl *Checklist) (*ChecklistItem, error) {
		item, err := cl.Resolve(selector)
		if err != nil {
			return nil, err
		}

		now := nowUTC()
		item.Status = status
		item.UpdatedAt = now
		if status.Terminal() {
			if item.CompletedAt == nil {
				completed := now
				item.CompletedAt = &completed
			}
		} else {
			item.CompletedAt = nil
		}
		return item, nil
	})
}

// EditChecklistItem updates the title, notes, and/or status of an item.
func (s *Service) EditChecklistItem(ctx context.Context, identifier, selector string, opts EditChecklistItemOptions) (*ChecklistMutationResult, error) {
	return s.mutateChecklist(ctx, identifier, func(cl *Checklist) (*ChecklistItem, error) {
		item, err := cl.Resolve(selector)
		if err != nil {
			return nil, err
		}

		if opts.Title != nil {
			title := strings.TrimSpace(*opts.Title)
			if title == "" {
				return nil, ErrEmptyChecklistTitle
			}
			item.Title = title
		}
		if opts.Notes != nil {
			item.Notes = strings.TrimSpace(*opts.Notes)
		}
		if opts.Status != nil {
			status := *opts.Status
			if !status.Valid() {
				return nil, ErrInvalidChecklistStatus
			}
			item.Status = status
			if status.Terminal() {
				if item.CompletedAt == nil {
					completed := nowUTC()
					item.CompletedAt = &completed
				}
			} else {
				item.CompletedAt = nil
			}
		}
		item.UpdatedAt = nowUTC()
		return item, nil
	})
}

// RankChecklistItem sets an item's explicit sort rank. Negative ranks are
// rejected: they would sort before the auto-assigned 10, 20, … sequence and
// only confuse operators.
func (s *Service) RankChecklistItem(ctx context.Context, identifier, selector string, rank int) (*ChecklistMutationResult, error) {
	if rank < 0 {
		return nil, ErrNegativeChecklistRank
	}
	return s.mutateChecklist(ctx, identifier, func(cl *Checklist) (*ChecklistItem, error) {
		item, err := cl.Resolve(selector)
		if err != nil {
			return nil, err
		}
		item.Rank = rank
		item.UpdatedAt = nowUTC()
		return item, nil
	})
}

// LinkChecklistItemWorkitem attaches a resolved workitem reference to an item.
func (s *Service) LinkChecklistItemWorkitem(ctx context.Context, identifier, selector string, wi *ChecklistWorkitem) (*ChecklistMutationResult, error) {
	normalized := normalizeWorkitem(wi)
	if normalized == nil {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "workitem reference is required")
	}
	return s.mutateChecklist(ctx, identifier, func(cl *Checklist) (*ChecklistItem, error) {
		item, err := cl.Resolve(selector)
		if err != nil {
			return nil, err
		}
		item.Workitem = normalized
		item.UpdatedAt = nowUTC()
		return item, nil
	})
}

// UnlinkChecklistItemWorkitem removes the workitem reference from an item.
func (s *Service) UnlinkChecklistItemWorkitem(ctx context.Context, identifier, selector string) (*ChecklistMutationResult, error) {
	return s.mutateChecklist(ctx, identifier, func(cl *Checklist) (*ChecklistItem, error) {
		item, err := cl.Resolve(selector)
		if err != nil {
			return nil, err
		}
		item.Workitem = nil
		item.UpdatedAt = nowUTC()
		return item, nil
	})
}

func normalizeWorkitem(wi *ChecklistWorkitem) *ChecklistWorkitem {
	if wi == nil {
		return nil
	}
	id := strings.TrimSpace(wi.ID)
	if id == "" {
		return nil
	}
	return &ChecklistWorkitem{ID: id, Ref: strings.TrimSpace(wi.Ref)}
}
