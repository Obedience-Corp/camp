package crawl

import (
	"context"
	"sort"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
)

// IntentStore is the narrow contract intent crawl needs from the
// intent service. It is satisfied by *intent.IntentService and by
// in-memory fakes used in tests.
type IntentStore interface {
	List(ctx context.Context, opts *intent.ListOptions) ([]*intent.Intent, error)
	Find(ctx context.Context, id string) (*intent.Intent, error)
	Save(ctx context.Context, in *intent.Intent) error
	Move(ctx context.Context, id string, newStatus intent.Status) (*intent.Intent, error)
	Count(ctx context.Context) ([]intent.StatusCount, int, error)
}

// SelectCandidates returns the intents to crawl, in the requested
// order, after limit is applied. Statuses must already be validated
// by Options.Validate.
//
// Implementation calls IntentStore.List once per candidate status
// and merges results, deduping by ID. This avoids relying on
// IntentService's internal multi-status branching, which today only
// supports a single-status filter.
func SelectCandidates(ctx context.Context, store IntentStore, opts Options) ([]*intent.Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	seen := make(map[string]struct{})
	var out []*intent.Intent
	for _, status := range opts.Statuses {
		s := status
		listOpts := &intent.ListOptions{Status: &s}
		intents, err := store.List(ctx, listOpts)
		if err != nil {
			return nil, camperrors.Wrapf(err, "listing intents for status %s", status)
		}
		for _, in := range intents {
			if _, dup := seen[in.ID]; dup {
				continue
			}
			seen[in.ID] = struct{}{}
			out = append(out, in)
		}
	}

	sortCandidates(out, opts.Sort)
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

func sortCandidates(in []*intent.Intent, mode string) {
	switch mode {
	case SortStale:
		sort.SliceStable(in, func(i, j int) bool {
			return staleLess(in[i], in[j])
		})
	case SortUpdated:
		sort.SliceStable(in, func(i, j int) bool {
			return effectiveUpdate(in[i]).After(effectiveUpdate(in[j]))
		})
	case SortCreated:
		sort.SliceStable(in, func(i, j int) bool {
			return in[i].CreatedAt.After(in[j].CreatedAt)
		})
	case SortPriority:
		sort.SliceStable(in, func(i, j int) bool {
			return priorityRank(in[i].Priority) > priorityRank(in[j].Priority)
		})
	case SortTitle:
		sort.SliceStable(in, func(i, j int) bool {
			return in[i].Title < in[j].Title
		})
	}
}

func staleLess(a, b *intent.Intent) bool {
	ea := effectiveUpdate(a)
	eb := effectiveUpdate(b)
	if !ea.Equal(eb) {
		return ea.Before(eb)
	}
	if a.Title != b.Title {
		return a.Title < b.Title
	}
	return a.ID < b.ID
}

func effectiveUpdate(in *intent.Intent) (t timeLike) {
	if !in.UpdatedAt.IsZero() {
		return timeLike{in.UpdatedAt.UnixNano()}
	}
	return timeLike{in.CreatedAt.UnixNano()}
}

// timeLike is a thin wrapper around int64 nanoseconds so we can
// expose Before/After/Equal without importing time everywhere.
// Using nanoseconds also gives stable, location-independent ordering.
type timeLike struct{ ns int64 }

func (t timeLike) Before(o timeLike) bool { return t.ns < o.ns }
func (t timeLike) After(o timeLike) bool  { return t.ns > o.ns }
func (t timeLike) Equal(o timeLike) bool  { return t.ns == o.ns }

func priorityRank(p intent.Priority) int {
	switch p {
	case intent.PriorityHigh:
		return 3
	case intent.PriorityMedium:
		return 2
	case intent.PriorityLow:
		return 1
	default:
		return 0
	}
}
