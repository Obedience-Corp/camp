package crawl

import (
	"context"
	"fmt"

	sharedcrawl "github.com/Obedience-Corp/camp/internal/crawl"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
)

// Result is what Run returns to the cobra command.
type Result struct {
	// Summary tallies keep/skip/move counts.
	Summary *sharedcrawl.Summary
	// Statuses reflects the live statuses that were actually
	// crawled (after defaulting). The caller uses this in the
	// "no candidates" message.
	Statuses []intent.Status
	// CommitPaths is the file/pre-staged set for the batch commit.
	// May be empty when no moves occurred.
	CommitPaths CommitPaths
	// CandidateCount is the number of intents the runner walked.
	CandidateCount int
}

// AuditAppender is the test seam for writing intent audit events.
// The default implementation calls audit.AppendEvent.
type AuditAppender func(ctx context.Context, intentsDir string, event audit.Event) error

// Config bundles dependencies for Run. Default constructors create
// production implementations; tests pass fakes.
type Config struct {
	// Store provides candidate listing and intent mutation.
	Store IntentStore
	// Prompt drives the user-facing interaction.
	Prompt sharedcrawl.Prompt
	// IntentsDir is the campaign-root-relative path to the intents
	// directory (e.g., ".campaign/intents"). Used for log/audit
	// path computation only; the store owns absolute filesystem
	// paths.
	IntentsDir string
	// Actor identifies the user/agent recording these decisions
	// in audit events. Empty allowed.
	Actor string
	// AppendAudit writes audit events. Defaults to audit.AppendEvent.
	AppendAudit AuditAppender
	// AppendLog writes crawl log entries. Defaults to DefaultLogAppender.
	AppendLog LogAppender
}

func (c *Config) defaults() {
	if c.AppendAudit == nil {
		c.AppendAudit = audit.AppendEvent
	}
	if c.AppendLog == nil {
		c.AppendLog = DefaultLogAppender
	}
}

// Run executes one crawl session. It validates opts, selects
// candidates, walks them through the prompt, applies mutations
// through the store, writes audit and log entries, and returns a
// Result the cobra command can render and commit.
//
// Errors from the prompt that match sharedcrawl.ErrAborted are
// converted into a normal early return: the partial Result is
// still returned so the caller can print a "Crawl cancelled" header
// followed by the summary.
//
// Hard errors after a save or a move (audit/log/move failures
// downstream) return immediately so the operator can investigate
// rather than silently continuing with partial state.
func Run(ctx context.Context, cfg Config, opts Options) (*Result, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	cfg.defaults()
	if cfg.Store == nil {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "intent crawl: store is required")
	}
	if cfg.Prompt == nil {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "intent crawl: prompt is required")
	}

	candidates, err := SelectCandidates(ctx, cfg.Store, opts)
	if err != nil {
		return nil, err
	}

	result := &Result{
		Summary:        sharedcrawl.NewSummary(),
		Statuses:       opts.Statuses,
		CandidateCount: len(candidates),
	}

	if len(candidates) == 0 {
		return result, nil
	}

	rawCounts, _, err := cfg.Store.Count(ctx)
	if err != nil {
		return result, camperrors.Wrap(err, "counting intents")
	}
	counts := countsByStatus(rawCounts)

	var sources []string

	for i, in := range candidates {
		if err := ctx.Err(); err != nil {
			return result, camperrors.Wrap(err, "context cancelled")
		}

		applied, sourcePath, destPath, err := processOne(ctx, cfg, in, counts, i+1, len(candidates), result.Summary)
		if err != nil {
			if sharedcrawl.IsAborted(err) {
				result.CommitPaths = BuildCommitPaths(flattenPaths(result.Summary), sources, cfg.IntentsDir)
				return result, sharedcrawl.ErrAborted
			}
			return result, err
		}
		switch applied {
		case appliedQuit:
			result.CommitPaths = BuildCommitPaths(flattenPaths(result.Summary), sources, cfg.IntentsDir)
			return result, nil
		case appliedMove:
			if sourcePath != "" {
				sources = append(sources, sourcePath)
			}
			// Update counts so the next destination picker reflects
			// the just-completed move.
			counts[in.Status]--
			counts[intent.Status(destPath.target)]++
		}
	}

	result.CommitPaths = BuildCommitPaths(flattenPaths(result.Summary), sources, cfg.IntentsDir)
	return result, nil
}

type appliedKind int

const (
	appliedNone appliedKind = iota
	appliedKeep
	appliedSkip
	appliedMove
	appliedQuit
)

type moveDest struct {
	target string
}

// processOne walks a single intent through the prompt loop and
// applies the resulting decision. It returns (applied, sourcePath,
// destination, err).
//
// sourcePath is the campaign-root-relative path of the intent file
// before the move (only meaningful when applied == appliedMove).
//
// destination.target is the target status string after a move
// (used by Run to refresh the destination counts).
func processOne(
	ctx context.Context,
	cfg Config,
	in *intent.Intent,
	counts map[intent.Status]int,
	idx, total int,
	summary *sharedcrawl.Summary,
) (appliedKind, string, moveDest, error) {
	item := sharedcrawl.Item{
		ID:          in.ID,
		Title:       PreviewTitle(idx, total, in),
		Description: PreviewDescription(in),
	}

	for {
		if err := ctx.Err(); err != nil {
			return appliedNone, "", moveDest{}, camperrors.Wrap(err, "context cancelled")
		}
		action, err := cfg.Prompt.SelectAction(ctx, item, firstStepOptions(in))
		if err != nil {
			return appliedNone, "", moveDest{}, err
		}

		switch action {
		case sharedcrawl.ActionQuit:
			return appliedQuit, "", moveDest{}, nil

		case sharedcrawl.ActionKeep:
			summary.RecordKeep()
			if err := cfg.AppendLog(ctx, cfg.IntentsDir, LogEntry{
				ID:       in.ID,
				Title:    in.Title,
				From:     in.Status,
				Decision: DecisionKeep,
			}); err != nil {
				return appliedNone, "", moveDest{}, camperrors.Wrap(err, "appending crawl log (keep)")
			}
			return appliedKeep, "", moveDest{}, nil

		case sharedcrawl.ActionSkip:
			summary.RecordSkip()
			if err := cfg.AppendLog(ctx, cfg.IntentsDir, LogEntry{
				ID:       in.ID,
				Title:    in.Title,
				From:     in.Status,
				Decision: DecisionSkip,
			}); err != nil {
				return appliedNone, "", moveDest{}, camperrors.Wrap(err, "appending crawl log (skip)")
			}
			return appliedSkip, "", moveDest{}, nil

		case sharedcrawl.ActionMove:
			dest, err := cfg.Prompt.SelectDestination(ctx, item, destinationOptions(in, counts))
			if err != nil {
				return appliedNone, "", moveDest{}, err
			}
			if dest.Target == "" {
				// User backed out of the destination picker; loop
				// back to the first-step menu.
				continue
			}
			target := intent.Status(dest.Target)
			if target == in.Status {
				// Treat redundant move as keep.
				summary.RecordKeep()
				if err := cfg.AppendLog(ctx, cfg.IntentsDir, LogEntry{
					ID:       in.ID,
					Title:    in.Title,
					From:     in.Status,
					Decision: DecisionKeep,
				}); err != nil {
					return appliedNone, "", moveDest{}, camperrors.Wrap(err, "appending crawl log (keep on no-op move)")
				}
				return appliedKeep, "", moveDest{}, nil
			}

			var reason string
			if dest.RequiresReason {
				reason, err = cfg.Prompt.Reason(ctx, item, dest)
				if err != nil {
					return appliedNone, "", moveDest{}, err
				}
				if reason == "" {
					// Empty reason for a dungeon move = cancel; loop
					// back to the first-step menu.
					continue
				}
			}

			sourcePath, destPath, mErr := applyMove(ctx, cfg, in, target, reason)
			if mErr != nil {
				return appliedNone, "", moveDest{}, mErr
			}
			summary.RecordMove(string(target), destPath)
			if err := cfg.AppendLog(ctx, cfg.IntentsDir, LogEntry{
				ID:       in.ID,
				Title:    in.Title,
				From:     in.Status,
				Decision: DecisionMove,
				To:       target,
				Reason:   reason,
			}); err != nil {
				return appliedNone, sourcePath, moveDest{target: dest.Target}, camperrors.Wrap(err, "appending crawl log (move)")
			}
			return appliedMove, sourcePath, moveDest{target: dest.Target}, nil
		}

		return appliedNone, "", moveDest{}, fmt.Errorf("intent crawl: unhandled action %q", action)
	}
}

// applyMove performs the intent-side mutation sequence:
//  1. Reload latest by ID.
//  2. For dungeon moves: append decision record, save.
//  3. Move via IntentService.Move.
//  4. Append audit event.
//
// Returns the source path before the move and the destination path
// after the move (both campaign-root-relative if the store returns
// them that way; the runner does not transform them).
func applyMove(
	ctx context.Context,
	cfg Config,
	in *intent.Intent,
	target intent.Status,
	reason string,
) (string, string, error) {
	latest, err := cfg.Store.Find(ctx, in.ID)
	if err != nil {
		return "", "", camperrors.Wrapf(err, "reloading intent %s", in.ID)
	}
	sourcePath := latest.Path
	prevStatus := latest.Status

	if target.InDungeon() {
		intent.AppendDecisionRecord(latest, target, reason)
		if err := cfg.Store.Save(ctx, latest); err != nil {
			return sourcePath, "", camperrors.Wrap(err, "saving decision record")
		}
	}

	moved, err := cfg.Store.Move(ctx, in.ID, target)
	if err != nil {
		return sourcePath, "", camperrors.Wrapf(err, "moving intent to %s", target)
	}

	if err := cfg.AppendAudit(ctx, cfg.IntentsDir, audit.Event{
		Type:   audit.EventMove,
		ID:     latest.ID,
		Title:  latest.Title,
		From:   string(prevStatus),
		To:     string(target),
		Reason: reason,
		Actor:  cfg.Actor,
	}); err != nil {
		return sourcePath, moved.Path, camperrors.Wrap(err, "writing audit event")
	}

	return sourcePath, moved.Path, nil
}

// flattenPaths returns the union of all destination paths recorded
// in summary. Order is destination-key sorted, then insertion order.
func flattenPaths(summary *sharedcrawl.Summary) []string {
	if summary == nil {
		return nil
	}
	keys := make([]string, 0, len(summary.Paths))
	for k := range summary.Paths {
		keys = append(keys, k)
	}
	// Determined order so commit list is stable.
	sortStrings(keys)
	out := make([]string, 0)
	for _, k := range keys {
		out = append(out, summary.Paths[k]...)
	}
	return out
}

func sortStrings(s []string) {
	// small alias to avoid pulling sort import name into runner.go
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
