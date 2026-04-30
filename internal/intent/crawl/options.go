// Package crawl implements the intent-specific crawl service.
//
// It walks live intents (inbox, ready, active), prompts the operator
// for a per-intent decision, applies moves through IntentService,
// records audit and crawl-log entries, and returns paths suitable
// for a batch git commit. Existing dungeon intents are never crawl
// candidates in v1.
package crawl

import (
	"fmt"
	"slices"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
)

// SortMode names the candidate ordering used by the crawl runner.
// It is a typed alias of string so the cobra flag boundary can
// convert raw user input exactly once and the rest of the package
// gets compile-time protection against typos.
type SortMode string

// Sort modes recognised by the crawl runner.
const (
	// SortStale orders intents by oldest effective update first.
	// Effective update is UpdatedAt when set, CreatedAt otherwise.
	// Title and ID are deterministic tie-breakers.
	SortStale SortMode = "stale"
	// SortUpdated orders by most-recently-updated first.
	SortUpdated SortMode = "updated"
	// SortCreated orders by most-recently-created first.
	SortCreated SortMode = "created"
	// SortPriority orders by highest priority first.
	SortPriority SortMode = "priority"
	// SortTitle orders by title ascending.
	SortTitle SortMode = "title"
)

// String returns the underlying string for use in error messages
// and serialization.
func (m SortMode) String() string { return string(m) }

// Options configures a single crawl session.
//
// Statuses, when set, replaces the default live scope. Each entry
// must be one of intent.StatusInbox, intent.StatusReady, or
// intent.StatusActive. Dungeon statuses are never valid candidate
// statuses in v1.
type Options struct {
	// Statuses restricts the candidate set. When nil/empty, the
	// default live scope (inbox, ready, active) is used.
	Statuses []intent.Status
	// Limit caps the number of candidates after sorting. Zero means
	// no limit. Negative is invalid.
	Limit int
	// Sort selects the candidate ordering. Empty value defaults to
	// SortStale.
	Sort SortMode
}

// Validate normalises and checks the options. It returns a wrapped
// camperrors.ErrInvalidInput on any validation failure so callers
// can surface a cobra-friendly error.
func (o *Options) Validate() error {
	if o.Limit < 0 {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "--limit must be non-negative")
	}
	if o.Sort == "" {
		o.Sort = SortStale
	}
	if !validSort(o.Sort) {
		return camperrors.Wrapf(
			camperrors.ErrInvalidInput,
			"invalid --sort %q (use stale, updated, created, priority, or title)",
			o.Sort,
		)
	}
	if len(o.Statuses) == 0 {
		o.Statuses = intent.ActiveStatuses()
		return nil
	}
	for _, s := range o.Statuses {
		if !isLiveStatus(s) {
			return camperrors.Wrapf(
				camperrors.ErrInvalidInput,
				"--status %q is not a live intent status (use inbox, ready, or active)",
				s,
			)
		}
	}
	return nil
}

// ParseStatusFlag parses a single user-provided value for the
// --status flag. Only the three live statuses are accepted; any
// dungeon spelling (short or canonical) is rejected with a clear
// message.
func ParseStatusFlag(raw string) (intent.Status, error) {
	cleaned := strings.TrimSpace(strings.ToLower(raw))
	switch cleaned {
	case "inbox":
		return intent.StatusInbox, nil
	case "ready":
		return intent.StatusReady, nil
	case "active":
		return intent.StatusActive, nil
	}
	switch {
	case cleaned == "done", cleaned == "killed", cleaned == "archived", cleaned == "someday",
		strings.HasPrefix(cleaned, "dungeon/"):
		return "", camperrors.Wrapf(
			camperrors.ErrInvalidInput,
			"--status %q is not a live status; intent crawl does not review dungeon intents",
			raw,
		)
	}
	return "", camperrors.Wrapf(
		camperrors.ErrInvalidInput,
		"unknown --status %q (use inbox, ready, or active)",
		raw,
	)
}

func validSort(s SortMode) bool {
	switch s {
	case SortStale, SortUpdated, SortCreated, SortPriority, SortTitle:
		return true
	}
	return false
}

func isLiveStatus(s intent.Status) bool {
	return slices.Contains(intent.ActiveStatuses(), s)
}

// ListSummary describes the candidate set selected for a crawl.
// It is exposed for "dry inspection" so the cobra command can show
// "no intents to crawl for statuses: ..." messages without driving
// the prompt.
type ListSummary struct {
	Statuses []intent.Status
	Total    int
}

// FormatNoCandidates returns the message printed when no intents
// match the requested statuses.
func FormatNoCandidates(statuses []intent.Status) string {
	parts := make([]string, 0, len(statuses))
	for _, s := range statuses {
		parts = append(parts, string(s))
	}
	return fmt.Sprintf("No intents to crawl for statuses: %s.", strings.Join(parts, ", "))
}
