package ledgerkit

import (
	"bytes"
	"context"
	"time"
)

// Filter narrows a ledger read by scope, kind, source, and time. Zero-valued
// fields do not constrain, so an empty Filter matches everything. Filters are
// composable: every set field must match (logical AND).
type Filter struct {
	// Scope predicates: when set, the event's corresponding scope field must
	// equal the value. Campaign is the common case (per-campaign boundary, D009).
	Campaign string
	Festival string
	Workitem string
	Intent   string
	Quest    string

	// Kinds, when non-empty, restricts to these event kinds.
	Kinds []Kind
	// Sources, when non-empty, restricts to these event sources.
	Sources []Source

	// Since/Until bound the event timestamp (inclusive). Zero means unbounded.
	Since time.Time
	Until time.Time
}

// Matches reports whether an event satisfies every set predicate.
func (f Filter) Matches(ev *Event) bool {
	if f.Campaign != "" && ev.Scope.Campaign != f.Campaign {
		return false
	}
	if f.Festival != "" && ev.Scope.Festival != f.Festival {
		return false
	}
	if f.Workitem != "" && ev.Scope.Workitem != f.Workitem {
		return false
	}
	if f.Intent != "" && ev.Scope.Intent != f.Intent {
		return false
	}
	if f.Quest != "" && ev.Scope.Quest != f.Quest {
		return false
	}
	if len(f.Kinds) > 0 && !containsKind(f.Kinds, ev.Kind) {
		return false
	}
	if len(f.Sources) > 0 && !containsSource(f.Sources, ev.Source) {
		return false
	}
	if !f.Since.IsZero() || !f.Until.IsZero() {
		ts := parseTS(ev.TS)
		if !f.Since.IsZero() && ts.Before(f.Since) {
			return false
		}
		if !f.Until.IsZero() && ts.After(f.Until) {
			return false
		}
	}
	return true
}

// monthOverlaps reports whether a shard for the given YYYY-MM month could hold
// any event inside the time window, so whole shards are skipped (predicate
// pushdown) before they are opened. Unparseable months are never skipped.
func (f Filter) monthOverlaps(month string) bool {
	if f.Since.IsZero() && f.Until.IsZero() {
		return true
	}
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return true
	}
	end := start.AddDate(0, 1, 0).Add(-time.Nanosecond) // last instant of the month
	if !f.Since.IsZero() && end.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && start.After(f.Until) {
		return false
	}
	return true
}

// Query returns every event matching the filter, ordered by ts then id and
// de-duplicated by id, along with a diagnostics report. Shards whose month
// cannot overlap the time window are skipped without being read; matching is
// applied per line so non-matching events are never retained.
func (r *Reader) Query(ctx context.Context, f Filter) ([]*Event, *ReadReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	refs, err := r.store.list()
	if err != nil {
		return nil, nil, err
	}
	report := &ReadReport{}
	var shards [][]*Event
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if !f.monthOverlaps(ref.Month) {
			continue
		}
		data, err := r.store.readFrom(ref.Rel, 0)
		if err != nil {
			return nil, nil, err
		}
		events, skipped, unknown := ParseShard(ref.Rel, bytes.NewReader(data))
		report.Skipped = append(report.Skipped, skipped...)
		report.UnknownVersions += unknown
		kept := events[:0]
		for _, ev := range events {
			if f.Matches(ev) {
				kept = append(kept, ev)
			}
		}
		shards = append(shards, kept)
	}
	merged, err := Merge(ctx, shards)
	if err != nil {
		return nil, nil, err
	}
	return merged, report, nil
}

func containsKind(kinds []Kind, k Kind) bool {
	for _, want := range kinds {
		if want == k {
			return true
		}
	}
	return false
}

func containsSource(sources []Source, s Source) bool {
	for _, want := range sources {
		if want == s {
			return true
		}
	}
	return false
}
