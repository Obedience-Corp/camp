package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/statusmove"
)

// Move moves an item from its current status to a new status.
// It validates the transition against schema rules unless Force is set.
// If the workflow has history tracking enabled, the move is logged.
func (s *Service) Move(ctx context.Context, item, to string, opts MoveOptions) (*MoveResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Load schema if not already loaded
	if s.schema == nil {
		if err := s.LoadSchema(ctx); err != nil {
			return nil, err
		}
	}

	// Validate destination status exists
	if !s.schema.HasDirectory(to) {
		return nil, camperrors.Wrap(ErrInvalidStatus, to)
	}

	// Find the item in the workflow
	from, itemPath, err := s.findItem(ctx, item)
	if err != nil {
		return nil, err
	}

	// Validate transition unless Force is set
	if !opts.Force && !s.schema.IsValidTransition(from, to) {
		return nil, camperrors.Wrapf(ErrInvalidTransition, "cannot move from %s to %s", from, to)
	}

	// Destination path
	destPath := resolveWorkflowDestinationPath(s.root, to, filepath.Base(itemPath), time.Now())

	movedPath, err := statusmove.Move(ctx, itemPath, destPath, statusmove.MoveOptions{BoundaryRoot: s.root})
	if err != nil {
		if errors.Is(err, statusmove.ErrAlreadyExists) {
			return nil, camperrors.Wrap(ErrAlreadyExists, destPath)
		}
		return nil, camperrors.Wrap(err, "failed to move item")
	}
	destPath = movedPath

	result := &MoveResult{
		Item:            item,
		From:            from,
		To:              to,
		Reason:          opts.Reason,
		SourcePath:      itemPath,
		DestinationPath: destPath,
	}

	// Log to history if enabled
	if s.schema.TrackHistory {
		entry := HistoryEntry{
			Item:   item,
			From:   from,
			To:     to,
			Reason: opts.Reason,
		}
		if err := s.appendHistory(ctx, entry); err != nil {
			// Log but don't fail the move
		}
	}

	return result, nil
}

// appendHistory adds an entry to the history file.
func (s *Service) appendHistory(ctx context.Context, entry HistoryEntry) error {
	// TODO: Implement history file writing
	return nil
}
