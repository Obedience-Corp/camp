// Package sync reconciles intents against the GitHub PRs referenced in their
// work_ref frontmatter (camp intent sync). Resolving PR state is the only I/O
// boundary (PRChecker); Plan is a pure decision table over that boundary, and
// Apply is the one function that mutates an intent, so the two can be tested
// independently: a fake PRChecker for the decision table, a containerized
// integration test for the filesystem move.
package sync

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
)

// prURLPattern matches a GitHub pull request URL, e.g.
// https://github.com/Obedience-Corp/camp/pull/123. The canonical URL is
// captured in group 1; anything after it (a trailing slash, /files, a
// diff-view query string, a #issuecomment-... fragment -- all common when a
// PR URL is copied straight from a browser address bar) is tolerated but
// discarded, since gh pr view expects the bare PR URL.
var prURLPattern = regexp.MustCompile(`^(https://github\.com/[^/\s]+/[^/\s]+/pull/\d+)(?:[/?#].*)?$`)

// PRURLFromRefs returns the canonical form of the first GitHub PR URL found
// in refs, or "" if none of the entries look like one.
func PRURLFromRefs(refs []string) string {
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if m := prURLPattern.FindStringSubmatch(ref); m != nil {
			return m[1]
		}
	}
	return ""
}

// Outcome classifies what sync decided for one intent.
type Outcome string

const (
	// OutcomeMerged means the tracked PR merged; the intent should auto-move
	// to dungeon/done.
	OutcomeMerged Outcome = "merged"

	// OutcomeClosed means the tracked PR closed without merging. Reported,
	// never auto-moved.
	OutcomeClosed Outcome = "closed"

	// OutcomeOpen means the tracked PR is still open. No action.
	OutcomeOpen Outcome = "open"

	// OutcomeCheckFailed means gh could not resolve the PR's state. Reported,
	// never auto-moved.
	OutcomeCheckFailed Outcome = "check_failed"
)

// Decision is the sync outcome for a single intent.
type Decision struct {
	IntentID string
	Title    string
	PRURL    string
	Outcome  Outcome
	Err      error
}

// Plan resolves PR state for every intent in candidates and returns one
// Decision per intent. candidates must already be filtered to non-dungeon
// intents with a resolvable PR URL (see PRURLFromRefs) — Plan does not
// re-filter, so its output is exactly one Decision per input intent.
//
// Plan performs no filesystem mutation; checker is its only I/O boundary, so
// unit tests exercise the full decision table against a fake.
func Plan(ctx context.Context, checker PRChecker, candidates []*intent.Intent) ([]Decision, error) {
	decisions := make([]Decision, 0, len(candidates))
	for _, i := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, camperrors.Wrap(err, "context cancelled")
		}

		prURL := PRURLFromRefs(i.WorkRef)
		d := Decision{IntentID: i.ID, Title: i.Title, PRURL: prURL}

		state, err := checker.State(ctx, prURL)
		if err != nil {
			d.Outcome = OutcomeCheckFailed
			d.Err = err
			decisions = append(decisions, d)
			continue
		}

		switch state {
		case PRStateMerged:
			d.Outcome = OutcomeMerged
		case PRStateClosed:
			d.Outcome = OutcomeClosed
		default:
			d.Outcome = OutcomeOpen
		}
		decisions = append(decisions, d)
	}
	return decisions, nil
}

// Apply performs the one filesystem-mutating consequence of a Decision:
// moving a merged intent to dungeon/done with a decision record capturing the
// merged PR. It is a no-op (returns nil, nil) for every other Outcome —
// closed/open/failed decisions are reported by the caller, never acted on.
func Apply(ctx context.Context, svc *intent.IntentService, d Decision) (*intent.Intent, error) {
	if d.Outcome != OutcomeMerged {
		return nil, nil
	}

	i, err := svc.Find(ctx, d.IntentID)
	if err != nil {
		return nil, err
	}

	reason := fmt.Sprintf("PR merged: %s", d.PRURL)
	intent.AppendDecisionRecord(i, intent.StatusDone, reason)
	if err := svc.Save(ctx, i); err != nil {
		return nil, camperrors.Wrap(err, "saving decision record")
	}

	return svc.Move(ctx, d.IntentID, intent.StatusDone)
}
