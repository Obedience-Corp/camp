package tui

import (
	"context"
	"io"
	"strconv"
	"time"

	"github.com/Obedience-Corp/camp/internal/crawl"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/triage"
)

// Row-card actions. The letters match spec doc 07 so muscle memory from
// dungeon crawl carries over.
const (
	actionApprove    crawl.Action = "approve"
	actionReject     crawl.Action = "reject"
	actionAmend      crawl.Action = "amend"
	actionOpen       crawl.Action = "open"
	actionSkip       crawl.Action = "skip"
	actionBack       crawl.Action = "back"
	actionQuit       crawl.Action = "quit"
	actionNextLane   crawl.Action = "next-lane"
	actionPickLane   crawl.Action = "pick-lane"
	actionApproveAll crawl.Action = "approve-lane"
	actionPriorities crawl.Action = "priorities"
)

// Flow drives the three-screen review.
//
// It records verdicts through the same store API `camp triage approve` uses.
// The TUI writes nothing itself, so a verdict entered here and one entered on
// the command line are the same event, recorded the same way.
type Flow struct {
	Store  *triage.Store
	RunID  string
	Prompt crawl.Prompt
	Out    io.Writer
	// Actor and Now are injected so a flow is reproducible under test.
	Actor string
	Now   func() time.Time
}

// Result reports what a session did.
type Result struct {
	Recorded int
	Skipped  int
	Quit     bool
}

// Run opens the review flow and returns when the user quits.
//
// Progress is never held in memory: every verdict appends immediately, so
// quitting at any point loses nothing and reopening resumes at the first row
// still awaiting a decision.
func (f *Flow) Run(ctx context.Context) (*Result, error) {
	result := &Result{}
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		done, err := f.summaryStep(ctx, result)
		if err != nil {
			if crawl.IsAborted(err) {
				result.Quit = true
				return result, nil
			}
			return result, err
		}
		if done {
			return result, nil
		}
	}
}

// summaryStep renders screen 1 and dispatches from it.
func (f *Flow) summaryStep(ctx context.Context, result *Result) (bool, error) {
	run, status, lanes, err := f.load(ctx)
	if err != nil {
		return false, err
	}
	f.write(Screen1(run, status, lanes))

	reviewable := reviewableLanes(lanes)
	options := []crawl.Option{}
	if len(reviewable) > 0 {
		options = append(options, crawl.Option{
			Label:  "Review next lane - " + reviewable[0].Title,
			Action: actionNextLane,
		})
		if len(reviewable) > 1 {
			options = append(options, crawl.Option{
				Label: "Pick a lane", Action: actionPickLane,
			})
		}
	}
	options = append(options,
		crawl.Option{Label: "Show priorities", Action: actionPriorities},
		crawl.Option{Label: "Quit - progress is saved", Action: actionQuit},
	)

	item := crawl.Item{
		ID:          run.ID,
		Title:       "Triage " + run.ID,
		Description: strconv.Itoa(status.Rows) + " rows · " + strconv.Itoa(len(lanes)) + " lanes",
	}
	action, err := f.Prompt.SelectAction(ctx, item, options)
	if err != nil {
		return false, err
	}

	switch action {
	case actionNextLane:
		if len(reviewable) == 0 {
			return true, nil
		}
		return f.laneStep(ctx, reviewable[0], result)
	case actionPickLane:
		lane, err := f.pickLane(ctx, reviewable)
		if err != nil || lane == nil {
			return false, err
		}
		return f.laneStep(ctx, *lane, result)
	case actionPriorities:
		in, err := triage.LoadRenderInput(ctx, f.Store, f.RunID)
		if err != nil {
			return false, err
		}
		f.write(string(triage.RenderPriorities(in)))
		return false, nil
	default:
		result.Quit = true
		return true, nil
	}
}

// pickLane runs the shared destination picker over the lane list, the same
// two-step grammar dungeon crawl uses for its status directories.
func (f *Flow) pickLane(ctx context.Context, lanes []triage.Lane) (*triage.Lane, error) {
	options := make([]crawl.Option, 0, len(lanes))
	for _, lane := range lanes {
		options = append(options, crawl.Option{
			Label:  lane.Title,
			Action: crawl.ActionMove,
			Target: triage.LaneSelectorName(lane.Title),
			Count:  len(lane.Rows),
		})
	}
	choice, err := f.Prompt.SelectDestination(ctx,
		crawl.Item{ID: f.RunID, Title: "Pick a lane"}, options)
	if err != nil {
		return nil, err
	}
	if choice.Target == "" {
		return nil, nil // backed out
	}
	for i := range lanes {
		if triage.LaneSelectorName(lanes[i].Title) == choice.Target {
			return &lanes[i], nil
		}
	}
	return nil, nil
}

// laneStep renders screen 2 and walks its rows.
func (f *Flow) laneStep(ctx context.Context, lane triage.Lane, result *Result) (bool, error) {
	for {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		run, _, lanes, err := f.load(ctx)
		if err != nil {
			return false, err
		}
		current := findLane(lanes, lane.Title)
		if current == nil {
			return false, nil
		}
		f.write(Screen2(*current, 0))

		pending := pendingRows(*current)
		options := []crawl.Option{}
		if len(pending) > 0 {
			options = append(options, crawl.Option{
				Label:  "Open next row - " + pending[0].Row.StableID,
				Action: actionNextLane,
			})
		}
		// Lane approval is offered only when it is actually allowed: a
		// terminal lane never gets one, and the profile's granularity has to
		// permit it. Offering a key that then refuses would be worse than not
		// offering it.
		if canApproveLane(run, *current) {
			options = append(options, crawl.Option{
				Label:  "Approve this whole lane",
				Action: actionApproveAll,
			})
		}
		options = append(options,
			crawl.Option{Label: "Back to summary", Action: actionBack},
			crawl.Option{Label: "Quit - progress is saved", Action: actionQuit},
		)

		item := crawl.Item{
			ID:          triage.LaneSelectorName(current.Title),
			Title:       current.Title,
			Description: strconv.Itoa(len(pending)) + " awaiting a verdict",
		}
		action, err := f.Prompt.SelectAction(ctx, item, options)
		if err != nil {
			return false, err
		}

		switch action {
		case actionNextLane:
			if len(pending) == 0 {
				return false, nil
			}
			quit, err := f.rowStep(ctx, *current, pending[0], result)
			if err != nil || quit {
				return quit, err
			}
		case actionApproveAll:
			if err := f.approveLane(ctx, *current, result); err != nil {
				return false, err
			}
		case actionBack:
			return false, nil
		default:
			result.Quit = true
			return true, nil
		}
	}
}

// approveLane records a verdict for every non-terminal row in a lane, after an
// explicit confirmation listing exactly what it covers.
func (f *Flow) approveLane(ctx context.Context, lane triage.Lane, result *Result) error {
	pending := pendingRows(lane)
	var covered []string
	for _, row := range pending {
		if !row.Verdict.CanonicalAction.Terminal() {
			covered = append(covered, row.Row.StableID)
		}
	}
	if len(covered) == 0 {
		f.write("Nothing in this lane can be approved in bulk.\n")
		return nil
	}

	f.write("Approving " + strconv.Itoa(len(covered)) + " row(s):\n")
	for _, id := range covered {
		f.write("  " + id + "\n")
	}

	confirmed, err := f.confirm(ctx, "Approve these "+strconv.Itoa(len(covered))+" rows?")
	if err != nil || !confirmed {
		return err
	}

	approval, err := f.Store.Approve(ctx, triage.ApproveInput{
		RunID:    f.RunID,
		Selector: triage.Selector{Lane: triage.LaneSelectorName(lane.Title)},
		Actor:    f.Actor,
		Now:      f.Now(),
	})
	if err != nil {
		return err
	}
	result.Recorded += len(approval.Recorded)
	f.write("Recorded " + strconv.Itoa(len(approval.Recorded)) + " verdict(s).\n")
	if len(approval.SkippedTerminal) > 0 {
		f.write("Skipped " + strconv.Itoa(len(approval.SkippedTerminal)) +
			" terminal row(s); approve them individually.\n")
	}
	return f.rerender(ctx)
}

// --- pure helpers ------------------------------------------------------

// reviewableLanes are the lanes with rows still awaiting a verdict, terminal
// lanes last so the reader builds context before irreversible calls.
func reviewableLanes(lanes []triage.Lane) []triage.Lane {
	var normal, terminal []triage.Lane
	for _, lane := range lanes {
		if len(pendingRows(lane)) == 0 {
			continue
		}
		if laneIsTerminal(lane) {
			terminal = append(terminal, lane)
			continue
		}
		normal = append(normal, lane)
	}
	return append(normal, terminal...)
}

// pendingRows are the rows in a lane that still need a human verdict.
func pendingRows(lane triage.Lane) []triage.LaneRow {
	out := make([]triage.LaneRow, 0, len(lane.Rows))
	for _, row := range lane.Rows {
		if row.Verdict.State == triage.VerdictProposed {
			out = append(out, row)
		}
	}
	return out
}

// canApproveLane reports whether the lane may be bulk-approved: never a
// terminal lane, and only when the profile's granularity allows it.
func canApproveLane(run *triage.Run, lane triage.Lane) bool {
	if laneIsTerminal(lane) {
		return false
	}
	switch run.Manifest.Profile.Resolved.Review.Approval {
	case triage.ApprovalLane, triage.ApprovalBatch:
		return len(pendingRows(lane)) > 0
	default:
		return false
	}
}

// findLane locates a lane by title after a reload.
func findLane(lanes []triage.Lane, title string) *triage.Lane {
	for i := range lanes {
		if lanes[i].Title == title {
			return &lanes[i]
		}
	}
	return nil
}

// ErrNoRun reports that a flow was opened on a campaign with no run.
var ErrNoRun = camperrors.Wrap(camperrors.ErrNotFound, "no triage run to review")
