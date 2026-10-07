package notify

import (
	"context"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/notice"
)

// Store is where the browser reads notices and records dismissals.
type Store interface {
	Inventory(ctx context.Context) (notice.Inventory, error)
	Dismiss(ctx context.Context, id string) error
	Restore(ctx context.Context, id string) error
}

// DiskStore is the Store over a camp's .campaign/notices.yaml, writing through
// the same dismissal API the dismiss and restore subcommands use.
type DiskStore struct {
	Root      string
	Detectors []notice.Detector
	Now       func() time.Time
}

// Inventory runs every detector and partitions the result against the
// dismissal file.
func (s DiskStore) Inventory(ctx context.Context) (notice.Inventory, error) {
	return notice.TakeInventory(ctx, s.Root, s.Detectors...)
}

// Dismiss records a dismissal. Dismissing an id twice is a no-op.
func (s DiskStore) Dismiss(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "dismiss notice")
	}
	dismissals, err := notice.LoadDismissals(s.Root)
	if err != nil {
		return err
	}
	if !dismissals.Dismiss(id, s.now()) {
		return nil
	}
	return dismissals.Save(s.Root)
}

// Restore removes a dismissal. Restoring an id that is not dismissed is a
// no-op.
func (s DiskStore) Restore(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "restore notice")
	}
	dismissals, err := notice.LoadDismissals(s.Root)
	if err != nil {
		return err
	}
	if !dismissals.Restore(id) {
		return nil
	}
	return dismissals.Save(s.Root)
}

func (s DiskStore) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
