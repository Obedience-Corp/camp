package jobs

import (
	"context"
	"fmt"
	"strings"

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
func fallbackMessage(ctx context.Context, repoPath string, job *Job, writerErr error) string {
	var b strings.Builder
	b.WriteString(fallbackSubject(ctx, repoPath, job))
	b.WriteString("\n\ncamp wrote this message itself: the configured commit message\n")
	b.WriteString("writer did not return one.\n")
	if reason := writerFailureReason(writerErr); reason != "" {
		b.WriteString("\n  ")
		b.WriteString(reason)
		b.WriteString("\n")
	}
	b.WriteString("\nReword it with: git commit --amend\n")
	return b.String()
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
