// Package drain is the wait-for-the-queue step every camp command that touches
// git history runs first.
//
// It lives in internal/ rather than cmd/camp/cmdutil because commands are
// spread across cmd/camp, cmd/camp/project, and internal/commands/*, and the
// last of those must not import a cmd package. The decision (jobs.Drain) and
// the copy (here) still stay apart, so camp's wording and fest's can differ
// without either reimplementing when to wait.
package drain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jobs"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// Waiting on the deferred queue, and what the user sees while it happens.
//
// The queue exists so camp's bookkeeping stops holding the terminal, and a
// drain is the one place that cost comes back. So the copy here follows the
// same rule as the staging guard's: camp says what it is waiting on by name,
// and any refusal carries its own way out. A user who is told "waiting..." with
// no subject and no escape learns to distrust the tool that made them wait.

// Mode selects how a command reacts to a drain that times out.
type Mode int

const (
	// Write is for commands that change or publish history: push, sync,
	// pull, fresh. A timeout refuses, because proceeding would leave the queued
	// commit behind in a way the user cannot see.
	Write Mode = iota
	// Read is for commands that only report: status, log, doctor. A
	// timeout warns and proceeds, because refusing to show status because a
	// commit is slow is worse than showing status with a caveat.
	Read
	// Commit is for camp commit and camp p commit. It refuses like
	// Write but waits job-aware first, so a deferred --auto-write message
	// writer running long does not refuse the user's next commit.
	Commit
	// Wait is for `camp jobs drain`, whose only purpose is the wait. It
	// refuses like Write but must not offer --no-drain: that flag exists so a
	// command can skip a wait it did not ask for, and this command is the
	// wait. Telling the user to rerun with a flag the command does not define
	// sends them into a usage error.
	Wait
)

// Repo waits for repoPath's deferred commits before a command that
// depends on them.
//
// It resolves the campaign and the lane itself, so a caller only has to know
// which repository it is about to touch. Outside a campaign, or with an empty
// queue, it is one directory read and returns zero.
//
// The returned duration is reported as drain_waited_ms by every --json command
// that drains, so an agent can see the queue's cost on its critical path rather
// than inferring it from total runtime.
func Repo(ctx context.Context, repoPath string, mode Mode) (time.Duration, error) {
	return drainRepoTo(ctx, os.Stderr, repoPath, mode)
}

// Note reports repoPath's outstanding commits without waiting for any of them.
//
// For commands that only report. Waiting made those commands pay the queue's
// full cost to buy a marginally fresher read: `camp status` is the thing a user
// runs constantly and often twice in a row, and holding it for up to
// DrainTimeout so its answer can be slightly more current is a bad trade
// against just saying how many commits are still on their way. The user re-runs
// it; that is what the command is for.
//
// The caveat the wait was protecting is kept, because it was the real point:
// the user is told the tree is mid-change, so a status that looks stale is
// explained rather than merely wrong. What changes is that they are told
// immediately instead of thirty seconds later.
//
// Returns the number of outstanding commits. Outside a campaign, or with an
// empty queue, it is one directory read and returns zero.
func Note(ctx context.Context, repoPath string) (int, error) {
	return noteTo(ctx, os.Stderr, repoPath)
}

func noteTo(ctx context.Context, w io.Writer, repoPath string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	campaignRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		// Not in a campaign: no queue, nothing to report. Campaign detection
		// problems belong to the command's own work, not to this notice.
		return 0, nil
	}
	repo := jobs.RepoForPath(campaignRoot, repoPath)
	if repo == "" {
		return 0, nil
	}
	outstanding, err := jobs.Outstanding(campaignRoot, repo)
	if err != nil {
		// A notice that cannot be computed must not fail the command it was
		// going to annotate.
		return 0, nil
	}
	if len(outstanding) == 0 {
		return 0, nil
	}
	// Report, but still kick the queue. Spawning is the cheap half of a drain
	// and the half that makes progress inevitable; waiting is the half that
	// costs the user their terminal. Taking only the second away would leave a
	// user whose worker died watching the same jobs sit there every time they
	// ran status.
	jobs.EnsureServed(ctx, campaignRoot, outstanding)
	_, _ = fmt.Fprintln(w, ui.Dim(pendingNotice(len(outstanding))))
	return len(outstanding), nil
}

// NoteAllLanes reports every lane's outstanding commits without waiting, for
// reporting commands that look at the whole campaign.
func NoteAllLanes(ctx context.Context, campaignRoot string) (int, error) {
	return noteAllTo(ctx, os.Stderr, campaignRoot)
}

func noteAllTo(ctx context.Context, w io.Writer, campaignRoot string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	outstanding, err := jobs.OutstandingAll(campaignRoot)
	if err != nil || len(outstanding) == 0 {
		return 0, nil
	}
	jobs.EnsureServed(ctx, campaignRoot, outstanding)
	_, _ = fmt.Fprintln(w, ui.Dim(pendingNotice(len(outstanding))))
	return len(outstanding), nil
}

// pendingNotice is what a reporting command prints instead of waiting.
//
// It does not reuse countPhrase, whose "1 queued commit" reads correctly in a
// refusal ("1 queued commit not completed after 30s") but stutters here into
// "1 queued commit still queued". This line prints on ordinary commands rather
// than only on failures, so it is the one a user sees most and the one least
// able to afford reading like a template.
func pendingNotice(n int) string {
	if n == 1 {
		return "1 commit still queued; what follows may not include it (camp jobs)"
	}
	return fmt.Sprintf("%d commits still queued; what follows may not include them (camp jobs)", n)
}

// CampaignRoot waits for the campaign root's lane. Commands that operate
// on the campaign itself rather than a specific repository use it.
func CampaignRoot(ctx context.Context, campaignRoot string, mode Mode) (time.Duration, error) {
	return drainTo(ctx, os.Stderr, campaignRoot, ".", mode)
}

// AllLanes waits for every lane in the campaign. Used by the commands that
// act on all repositories at once, where draining only one would let another's
// queued commit land after the run meant to publish it.
func AllLanes(ctx context.Context, campaignRoot string, mode Mode) (time.Duration, error) {
	return drainAllTo(ctx, os.Stderr, campaignRoot, mode)
}

// drainRepoTo is DrainRepo with the output stream injected, for tests.
func drainRepoTo(ctx context.Context, w io.Writer, repoPath string, mode Mode) (time.Duration, error) {
	campaignRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		// Not in a campaign: there is no queue, so there is nothing to wait
		// for. A drain is not the place to report campaign detection problems;
		// the command's own work will.
		return 0, nil
	}
	repo := jobs.RepoForPath(campaignRoot, repoPath)
	if repo == "" {
		return 0, nil
	}
	return drainTo(ctx, w, campaignRoot, repo, mode)
}

func drainTo(ctx context.Context, w io.Writer, campaignRoot, repo string, mode Mode) (time.Duration, error) {
	result, err := jobs.Drain(ctx, campaignRoot, repo, optionsFor(w, mode))
	return report(w, mode, result, err)
}

func drainAllTo(ctx context.Context, w io.Writer, campaignRoot string, mode Mode) (time.Duration, error) {
	result, err := jobs.DrainAll(ctx, campaignRoot, optionsFor(w, mode))
	return report(w, mode, result, err)
}

func optionsFor(w io.Writer, mode Mode) jobs.DrainOptions {
	return jobs.DrainOptions{
		JobAware:  mode == Commit,
		OnWaiting: func(s jobs.DrainStatus) { _, _ = fmt.Fprintln(w, ui.Dim(waitingLine(s))) },
	}
}

// report turns a drain outcome into what the user sees and what the command
// does next. Both drain entrypoints share it so a read-only command cannot
// accidentally refuse, or a write command accidentally proceed, depending on
// which one it called.
func report(w io.Writer, mode Mode, result jobs.DrainResult, err error) (time.Duration, error) {
	var timeout *jobs.DrainTimeoutError
	if errors.As(err, &timeout) {
		if mode == Read {
			_, _ = fmt.Fprintln(w, ui.Warning(timeoutWarning(timeout)))
			return result.Waited, nil
		}
		return result.Waited, camperrors.New(refusal(timeout, mode))
	}
	return result.Waited, err
}

// verbFor names the operation in a refusal, so the message says what will not
// happen rather than that something went wrong.
func verbFor(mode Mode) string {
	switch mode {
	case Commit:
		return "Committing"
	case Wait:
		return "The queue did not finish"
	default:
		return "Continuing"
	}
}

// waitingLine is the line shown once a drain has been waiting long enough to be
// worth mentioning.
func waitingLine(s jobs.DrainStatus) string {
	return fmt.Sprintf("  waiting on %s (%s)...",
		countPhrase(len(s.Blocking)), jobs.Describe(s.Blocking[0]))
}

// timeoutWarning is what a read-only command prints when the queue outlasts it.
func timeoutWarning(e *jobs.DrainTimeoutError) string {
	return fmt.Sprintf("%s still queued after %s; what follows may be out of date (camp jobs)",
		countPhrase(len(e.Blocking)), e.Waited.Round(time.Second))
}

// refusal is what a write command prints instead of proceeding.
//
// It names the blocking jobs and offers three ways out, because a refusal whose
// only option is "wait" is a wedge. Every option is a command the user can run
// as printed.
func refusal(e *jobs.DrainTimeoutError, mode Mode) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s not completed after %s\n\n",
		countPhrase(len(e.Blocking)), e.Waited.Round(time.Second))
	for _, job := range e.Blocking {
		fmt.Fprintf(&b, "  %s", jobs.Describe(job))
		// Never failed here: a drain only ever waits on pending and running
		// jobs, so the count is always forward-looking.
		if note := jobs.AttemptNote(job.Attempts, false); note != "" {
			fmt.Fprintf(&b, " (%s)", note)
		}
		b.WriteString("\n")
	}
	if mode == Wait {
		fmt.Fprintf(&b, "\n%s. Options:\n\n", verbFor(mode))
	} else {
		fmt.Fprintf(&b, "\n%s now would leave them behind. Options:\n\n", verbFor(mode))
	}
	b.WriteString("  see what is stuck   camp jobs\n")
	// Only offered where it exists. `camp jobs drain` is the wait, so there is
	// nothing for it to skip.
	if mode != Wait {
		b.WriteString("  continue anyway     rerun with --no-drain\n")
	}
	b.WriteString("  run it here         camp jobs run\n")
	b.WriteString("  give up on them     camp jobs drop <id>")
	return b.String()
}

// countPhrase renders "1 queued commit" or "N queued commits".
func countPhrase(n int) string {
	if n == 1 {
		return "1 queued commit"
	}
	return fmt.Sprintf("%d queued commits", n)
}
