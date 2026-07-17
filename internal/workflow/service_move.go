package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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
	destPath, err := resolveWorkflowDestinationPath(ctx, s.root, to, filepath.Base(itemPath), s.dungeonSpelling, time.Now())
	if err != nil {
		return nil, camperrors.Wrapf(err, "resolving destination for %s", to)
	}

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
	if err := ctx.Err(); err != nil {
		return err
	}

	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}

	historyFile := s.schema.HistoryFile
	if historyFile == "" {
		historyFile = DefaultHistoryFile
	}
	historyPath := filepath.Join(s.root, historyFile)

	if err := os.MkdirAll(filepath.Dir(historyPath), 0755); err != nil {
		return camperrors.Wrap(err, "create history directory")
	}

	f, err := os.OpenFile(historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return camperrors.Wrap(err, "open history file")
	}
	defer func() { _ = f.Close() }()

	data, err := json.Marshal(entry)
	if err != nil {
		return camperrors.Wrap(err, "marshal history entry")
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return camperrors.Wrap(err, "write history entry")
	}
	return nil
}
