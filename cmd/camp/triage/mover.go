package triage

import (
	"context"
	"io"
	"path"

	wicommands "github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/intent"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/triage"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
)

// serviceMover executes planned commands through camp's existing services.
//
// Spec doc 01's no-new-movers rule in practice: nothing here moves a directory
// or edits a store itself. Attention goes through the same priority store the
// `camp workitem stage` command writes, and promotion goes through the exact
// function `camp workitem promote` runs. Re-entering camp as a subprocess
// would have been easier and would have thrown away the destination and the
// error the seam returns.
type serviceMover struct {
	campaignRoot string
	cfg          *config.CampaignConfig
	// out receives whatever the underlying services print. Apply renders its
	// own transcript, so this is normally discarded.
	out io.Writer
}

// Stage sets a workitem's attention stage.
func (m *serviceMover) Stage(ctx context.Context, stableID, stage string) (triage.MoveOutcome, error) {
	item, err := selector.Resolve(ctx, m.campaignRoot, stableID, selector.ResolveOptions{})
	if err != nil {
		return triage.MoveOutcome{}, err
	}
	if !priority.EligibleForAttention(*item) {
		return triage.MoveOutcome{}, camperrors.NewValidation("workitem",
			"attention stage is only supported for directory-backed workflow workitems", nil)
	}

	resolver := paths.NewResolverFromConfig(m.campaignRoot, m.cfg)
	items, err := wkitem.Discover(ctx, m.campaignRoot, resolver)
	if err != nil {
		return triage.MoveOutcome{}, camperrors.Wrap(err, "discovering work items")
	}

	// The undo is read before the write, because afterwards the previous
	// stage is gone and the receipt would have nothing true to record.
	//
	// It has to come from the priority store, not from the resolved item: a
	// stage lives in the store, and selector.Resolve returns the on-disk
	// workitem without that overlay. Reading it off the item recorded an undo
	// of `clear` for every row, which would restore a parked workitem to no
	// stage at all rather than to the stage it actually had.
	previousStore, err := priority.Load(priority.StorePath(m.campaignRoot))
	if err != nil {
		return triage.MoveOutcome{}, camperrors.Wrap(err, "loading the priority store")
	}
	overlaid := *item
	priority.ApplyAttentionToItem(previousStore, &overlaid)
	previous := overlaid.AttentionStage
	if previous == "" {
		previous = "clear"
	}

	clear := stage == "clear" || stage == ""
	if err := priority.WithLock(ctx, priority.StorePath(m.campaignRoot), func(store *priority.Store) error {
		if clear {
			priority.ClearAttentionStage(store, item.Key)
		} else {
			priority.SetAttentionStage(store, item.Key, priority.AttentionStage(stage))
		}
		priority.Prune(store, priority.ValidKeys(items))
		return nil
	}); err != nil {
		return triage.MoveOutcome{}, camperrors.Wrap(err, "updating workitem attention stage")
	}

	return triage.MoveOutcome{
		Undo: "camp workitem stage " + stableID + " " + previous,
	}, nil
}

// Promote moves a workitem along the rail or into a dungeon status.
func (m *serviceMover) Promote(ctx context.Context, stableID, target string) (triage.MoveOutcome, error) {
	outcome, err := wicommands.PromoteWorkitem(ctx, m.out, stableID, target)
	if err != nil {
		return triage.MoveOutcome{}, err
	}

	result := triage.MoveOutcome{}
	// The real undo, built from where the workitem actually landed rather
	// than from where the plan guessed it would. A dungeon destination
	// carries a dated bucket the compiler cannot know.
	if outcome.PromotedTo != "" && outcome.From != "" {
		result.Undo = "camp move " + path.Clean(outcome.PromotedTo) + " " + path.Clean(outcome.From)
	}
	if outcome.Committed {
		// Read after the commit rather than threaded through it. The commit
		// plumbing (commit.Crawl -> DungeonMoveCommitOutcome) does not carry a
		// hash today, and adding one touches every dungeon and workitem mover
		// that shares it. A campaign is single-user and this read happens
		// immediately after the commit it describes, so the window is not a
		// practical race; the cleaner fix is recorded in the review.
		result.Commit = git.HeadSHA(ctx, m.campaignRoot)
	}
	return result, nil
}

// Split runs the same verb `camp workitem split` runs, through its own
// service seam rather than by re-entering camp.
//
// The undo is the split's own inverse. It is recorded on the receipt like any
// other action, and it is safe by construction: --undo deletes only successors
// it can prove nobody touched.
func (m *serviceMover) Split(ctx context.Context, stableID string, successors []string) (triage.MoveOutcome, error) {
	if len(successors) == 0 {
		return triage.MoveOutcome{}, camperrors.NewValidation("successors",
			"a consolidate verdict must declare successors in its rationale", camperrors.ErrInvalidInput)
	}
	if err := wicommands.SplitWorkitem(ctx, m.out, stableID, successors); err != nil {
		return triage.MoveOutcome{}, err
	}
	return triage.MoveOutcome{
		Undo: "camp workitem split " + stableID + " --undo",
	}, nil
}

// MoveIdea moves a file-backed workitem through the idea lifecycle.
//
// The same no-new-movers rule the rest of this file follows: this is the
// service `camp idea move` runs, called directly, so the destination and the
// error come back rather than being parsed out of a subprocess's output.
//
// Directory-backed workitems are promoted and file-backed ones are moved, and
// neither service accepts the other's items — `camp workitem stage` refuses an
// idea outright. Routing on the row's recorded item kind is what makes an
// approved verdict on an intent executable at all.
func (m *serviceMover) MoveIdea(ctx context.Context, stableID, status, reason string) (triage.MoveOutcome, error) {
	resolver := paths.NewResolverFromConfig(m.campaignRoot, m.cfg)
	svc := intent.NewIntentService(m.campaignRoot, resolver.Intents())

	target := intent.Status(status)
	if !target.InDungeon() {
		// `camp idea move` accepts short dungeon names; the service wants the
		// canonical path form, so anything not already canonical is resolved
		// against the vocabulary rather than string-built.
		if canonical, ok := canonicalIdeaStatus(status); ok {
			target = canonical
		}
	}

	before, err := svc.Find(ctx, stableID)
	if err != nil {
		return triage.MoveOutcome{}, camperrors.Wrapf(err, "finding idea %s", stableID)
	}
	previous := before.Status

	// The decision record is appended before the move, the way `camp idea
	// move` does it, so a retired idea carries why in its own body rather than
	// only in the run's receipts.
	if target.InDungeon() && reason != "" {
		intent.AppendDecisionRecord(before, target, reason)
		if err := svc.Save(ctx, before); err != nil {
			return triage.MoveOutcome{}, camperrors.Wrap(err, "recording the decision on the idea")
		}
	}

	if _, err := svc.Move(ctx, stableID, target); err != nil {
		return triage.MoveOutcome{}, camperrors.Wrapf(err, "moving idea %s to %s", stableID, target)
	}

	return triage.MoveOutcome{
		Undo: "camp idea move " + stableID + " " + shortStatus(previous),
	}, nil
}

// canonicalIdeaStatus resolves a short status name to camp's canonical form.
func canonicalIdeaStatus(status string) (intent.Status, bool) {
	for _, candidate := range []intent.Status{
		intent.StatusInbox, intent.StatusReady, intent.StatusActive,
		intent.StatusDone, intent.StatusKilled, intent.StatusArchived, intent.StatusSomeday,
	} {
		if string(candidate) == status || shortStatus(candidate) == status {
			return candidate, true
		}
	}
	return "", false
}

// shortStatus renders a status the way the CLI accepts it.
func shortStatus(status intent.Status) string {
	return path.Base(string(status))
}
