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

// DrainMode selects how a command reacts to a drain that times out.
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
	// DrainWrite but waits job-aware first, so a deferred --auto-write message
	// writer running long does not refuse the user's next commit.
	Commit
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
		return result.Waited, camperrors.New(refusal(timeout, verbFor(mode)))
	}
	return result.Waited, err
}

// verbFor names the operation in a refusal, so the message says what will not
// happen rather than that something went wrong.
func verbFor(mode Mode) string {
	if mode == Commit {
		return "Committing"
	}
	return "Continuing"
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
func refusal(e *jobs.DrainTimeoutError, verb string) string {
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
	fmt.Fprintf(&b, "\n%s now would leave them behind. Options:\n\n", verb)
	b.WriteString("  see what is stuck   camp jobs\n")
	b.WriteString("  continue anyway     rerun with --no-drain\n")
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
