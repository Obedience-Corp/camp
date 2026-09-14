package jobs

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/autowrite"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// What a deferred commit says when the configured message writer could not say
// it.
//
// Camp used to park these jobs rather than describe them itself, on the
// reasoning that a subject camp invented was worse than a commit the user could
// retry. A month of running that way settled it the other way. The writer here
// is an LLM behind a local daemon, and it is unavailable for entirely ordinary
// reasons — a cold model, an idle-timeout, a daemon that is not up — none of
// which say anything about whether the user's work should be committed. Six
// commits were lost to it in this campaign's worker log alone, and the loss is
// not a pause: a parked commit-tree job decays, because every later commit
// makes its captured parent staler, and once the captured change conflicts with
// what landed the job can never be retried at all.
//
// So the tree wins over the subject. What lands is deterministic, derived from
// the diff the job actually carries, and marked so the user can find it again.
//
// What changed since is when this runs, never whether. A writer that reports
// itself temporarily unavailable buys the job a bounded wait first
// (writerwait.go), because "the daemon is not up" is an outage rather than a
// verdict on the commit. At the end of that window this is still what lands,
// and the body says how long camp waited.

// FallbackSubjectMarker tags the subject of a commit camp had to describe
// itself, so `git log --oneline | grep` finds every one of them.
//
// Exported because it is a promise to the user rather than an implementation
// detail: the whole value of a degraded commit landing is that the user can
// find it later and reword it.
const FallbackSubjectMarker = "(writer unavailable)"

// maxFallbackSubject bounds the generated subject, leaving room for the
// campaign tag camp prepends afterwards.
const maxFallbackSubject = 60

// fallbackMessage describes a commit whose message writer did not produce one.
// Deterministic by necessity: it runs precisely when the configured writer is
// unavailable, so it may use only what git already knows. The body names the
// cause because a background worker has no other way to tell anyone, and a
// commit the user cannot explain later is its own small mystery.
func fallbackMessage(ctx context.Context, repoPath string, job *Job, writerErr error, now time.Time) string {
	return fallbackSubject(ctx, repoPath, job) + "\n\n" +
		fallbackBody(writerFailureReason(writerErr), writerWaitSummary(job, writerErr, now))
}

// writerWaited says how long camp kept asking an unavailable writer before
// giving up, or nil when it never waited at all.
type writerWaited struct {
	// For is the elapsed time since the first unavailable attempt.
	For time.Duration
	// Attempts is how many times the writer said it was unavailable.
	Attempts int
}

// writerWaitSummary reconstructs the wait from the job document, and returns
// nil when there was none.
//
// The attempt that is landing this commit counts only when it too found the
// writer unavailable. A writer that was down for half an hour and then came
// back broken did not go unreachable a final time, and a body that said so
// would send the user looking for an outage that had already ended.
func writerWaitSummary(job *Job, writerErr error, now time.Time) *writerWaited {
	since, ok := jobTime(job.WriterUnavailableSince)
	if !ok {
		return nil
	}
	attempts := job.WriterAttempts
	if autowrite.WriterUnavailable(writerErr) {
		attempts++
	}
	return &writerWaited{For: max(now.Sub(since), 0).Round(time.Second), Attempts: attempts}
}

// fallbackBody renders everything under the subject of a degraded commit.
//
// Split from the git call above so the text a user is left with forever is
// testable without a repository, the same reason fallbackSubjectFor is.
func fallbackBody(reason string, waited *writerWaited) string {
	var b strings.Builder
	b.WriteString("camp wrote this message itself: the configured commit message\n")
	b.WriteString("writer did not return one.\n")
	if waited != nil {
		// Named in the commit because this is the only place it survives: the
		// worker log is a cache file, and a user reading `git log` a week later
		// has nothing else to tell "camp gave up instantly" from "camp waited
		// an hour for your daemon and it never came back".
		fmt.Fprintf(&b, "\ncamp waited for it: the writer was unreachable for %s across %s.\n",
			waited.For, attemptPhrase(waited.Attempts))
	}
	if reason != "" {
		b.WriteString("\n  ")
		b.WriteString(reason)
		b.WriteString("\n")
	}
	b.WriteString("\nReword it with: git commit --amend\n")
	return b.String()
}

// attemptPhrase renders an attempt count in prose.
func attemptPhrase(n int) string {
	if n == 1 {
		return "1 attempt"
	}
	return fmt.Sprintf("%d attempts", n)
}

// fallbackSubject names what the commit contains, in the shape a person scans.
func fallbackSubject(ctx context.Context, repoPath string, job *Job) string {
	paths, err := git.TreeChangedPaths(ctx, repoPath, job.Parent, job.Tree)
	if err != nil {
		// A subject is still owed even when git cannot say what changed. The
		// commit is landing either way, and one that says nothing is better
		// than no commit at all.
		return fallbackSubjectFor(nil)
	}
	return fallbackSubjectFor(paths)
}

// fallbackSubjectFor renders the subject for a set of changed paths.
//
// One changed path is worth naming outright; several are not, because a
// subject listing them is longer than the terminal and less informative than
// the count. A path long enough to blow the subject line degrades to the count
// form rather than being truncated into something that reads like a real path
// but is not one.
//
// Split from its git call so the shape of the subject, which is the part a
// user sees forever, is testable without a repository behind it.
func fallbackSubjectFor(paths []string) string {
	switch len(paths) {
	case 0:
		return "Deferred commit " + FallbackSubjectMarker
	case 1:
		subject := "Update " + paths[0] + " " + FallbackSubjectMarker
		if len(subject) <= maxFallbackSubject {
			return subject
		}
		return "Update 1 file " + FallbackSubjectMarker
	default:
		return fmt.Sprintf("Update %d files %s", len(paths), FallbackSubjectMarker)
	}
}

// writerFailureReason returns the writer's own diagnostic when the failure
// came from the writer, and nothing when it did not.
//
// Only a *autowrite.WriterError carries a reason that is safe to embed: it has
// been stripped of escape codes and bounded. Any other error is camp's own and
// its text is not written into git history.
func writerFailureReason(err error) string {
	var writerErr *autowrite.WriterError
	if camperrors.As(err, &writerErr) {
		return writerErr.Reason
	}
	return ""
}
