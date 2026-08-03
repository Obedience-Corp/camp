package drain

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/jobs"
)

// This package is copy and one decision: whether a slow queue stops the command
// or merely annotates it. Both halves are worth testing directly, because
// getting the decision backwards is silent in either direction. A read-only
// command that refuses looks like camp is broken; a write command that proceeds
// leaves a commit unpushed and says nothing.

func timeoutErr(jobList ...jobs.Job) *jobs.DrainTimeoutError {
	return &jobs.DrainTimeoutError{
		Repo:     ".",
		Blocking: jobList,
		Waited:   30 * time.Second,
	}
}

// A read-only command proceeds. Refusing to show status because a commit is
// slow is worse than showing status with a caveat.
func TestReadModeWarnsAndProceeds(t *testing.T) {
	var out bytes.Buffer
	waited, err := report(&out, Read, jobs.DrainResult{Waited: 30 * time.Second},
		timeoutErr(jobs.Job{Kind: jobs.KindCommitPaths, Repo: ".", Message: "capture intent: slow"}))

	if err != nil {
		t.Fatalf("read mode returned %v; a slow queue must not stop a read-only command", err)
	}
	if waited != 30*time.Second {
		t.Errorf("waited = %v, want the drain's own measurement", waited)
	}
	text := out.String()
	if !strings.Contains(text, "still queued") {
		t.Errorf("warning did not say the report may be stale:\n%s", text)
	}
	if !strings.Contains(text, "camp jobs") {
		t.Errorf("warning did not name where to look:\n%s", text)
	}
}

// A write command refuses. Proceeding would leave the queued commit behind in
// a way the user cannot see.
func TestWriteAndCommitModesRefuse(t *testing.T) {
	for _, mode := range []Mode{Write, Commit, Wait} {
		var out bytes.Buffer
		_, err := report(&out, mode, jobs.DrainResult{},
			timeoutErr(jobs.Job{Kind: jobs.KindCommitPaths, Repo: ".", Message: "capture intent: blocking"}))
		if err == nil {
			t.Fatalf("mode %v proceeded past a timeout; a write command must refuse", mode)
		}
		if !strings.Contains(err.Error(), "capture intent: blocking") {
			t.Errorf("refusal did not name the blocking job:\n%s", err)
		}
	}
}

// Every refusal carries its own way out. A refusal whose only option is "wait"
// is a wedge, and each option here is a command the user can run as printed.
func TestRefusalOffersAWayOut(t *testing.T) {
	text := refusal(timeoutErr(
		jobs.Job{Kind: jobs.KindCommitPaths, Repo: ".", Message: "capture intent: one"},
		jobs.Job{Kind: jobs.KindCommitPaths, Repo: ".", Message: "capture intent: two"},
	), Write)

	for _, want := range []string{
		"capture intent: one",
		"capture intent: two",
		"camp jobs",
		"--no-drain",
		"camp jobs drop",
		"Continuing now would leave them behind",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal missing %q:\n%s", want, text)
		}
	}
}

// `camp jobs drain` must not be told to rerun with --no-drain: the flag exists
// so a command can skip a wait it did not ask for, and this command is the
// wait. It has no such flag, so the advice sends the user into a usage error.
func TestWaitModeDoesNotOfferAFlagItDoesNotHave(t *testing.T) {
	text := refusal(timeoutErr(
		jobs.Job{Kind: jobs.KindCommitPaths, Repo: ".", Message: "capture intent: wedged"},
	), Wait)

	if strings.Contains(text, "--no-drain") {
		t.Errorf("the drain command offered a flag it does not define:\n%s", text)
	}
	if !strings.Contains(text, "The queue did not finish") {
		t.Errorf("refusal should say the wait did not finish, not that continuing loses work:\n%s", text)
	}
	for _, want := range []string{"camp jobs", "camp jobs run", "camp jobs drop"} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal missing %q:\n%s", want, text)
		}
	}
}

// Counts agree in number. The same class of bug shipped once already in this
// work, where a test asserting a substring passed against the wrong plural.
func TestCountPhraseAgreesInNumber(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{1, "1 queued commit"},
		{2, "2 queued commits"},
	}
	for _, tt := range tests {
		if got := countPhrase(tt.n); got != tt.want {
			t.Errorf("countPhrase(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
	// Asserted as an exact match rather than a substring: "1 queued commit"
	// is a prefix of "1 queued commits", so Contains would accept the bug.
	if countPhrase(1) == countPhrase(2) {
		t.Error("singular and plural must differ")
	}
}

// The waiting line names the work rather than saying "please wait", so a user
// who sees it repeatedly learns what camp is doing on their behalf.
func TestWaitingLineNamesTheWork(t *testing.T) {
	line := waitingLine(jobs.DrainStatus{
		Blocking: []jobs.Job{
			{Kind: jobs.KindCommitPaths, Repo: ".", Message: "artifact manifest for videos"},
			{Kind: jobs.KindCommitPaths, Repo: ".", Message: "second job"},
		},
		Elapsed: time.Second,
	})

	if !strings.Contains(line, "2 queued commits") {
		t.Errorf("waiting line did not say how many: %q", line)
	}
	if !strings.Contains(line, "artifact manifest for videos") {
		t.Errorf("waiting line did not name the work: %q", line)
	}
	if strings.Contains(strings.ToLower(line), "please wait") {
		t.Errorf("waiting line should name the work, not ask for patience: %q", line)
	}
}

// Only the commit path waits job-aware. Every command inheriting the extension
// would mean a wedged writer holds up a push for five minutes instead of
// refusing in thirty seconds with something the user can act on.
func TestOnlyCommitModeIsJobAware(t *testing.T) {
	var out bytes.Buffer
	tests := []struct {
		mode Mode
		want bool
	}{
		{Write, false},
		{Read, false},
		{Wait, false},
		{Commit, true},
	}
	for _, tt := range tests {
		if got := optionsFor(&out, tt.mode).JobAware; got != tt.want {
			t.Errorf("mode %v JobAware = %v, want %v", tt.mode, got, tt.want)
		}
	}
}

// A refusal names the operation that will not happen, so the message says what
// the user loses rather than that something went wrong.
func TestVerbNamesTheOperation(t *testing.T) {
	if got := verbFor(Commit); got != "Committing" {
		t.Errorf("verbFor(Commit) = %q, want the commit verb", got)
	}
	if got := verbFor(Write); got != "Continuing" {
		t.Errorf("verbFor(Write) = %q", got)
	}
	if got := verbFor(Wait); got == "Continuing" {
		t.Error("the drain command needs its own verb; it is the wait, not something the wait blocks")
	}
}

// A non-timeout error passes through untouched. Turning a real failure into a
// warning would hide it behind copy written for a slow queue.
func TestNonTimeoutErrorsAreNotSwallowed(t *testing.T) {
	var out bytes.Buffer
	boom := errBoom{}
	for _, mode := range []Mode{Read, Write, Commit, Wait} {
		_, err := report(&out, mode, jobs.DrainResult{}, boom)
		if err != boom {
			t.Errorf("mode %v turned a real error into %v", mode, err)
		}
	}
	if out.Len() != 0 {
		t.Errorf("a real error must not print drain copy:\n%s", out.String())
	}
}

type errBoom struct{}

func (errBoom) Error() string { return "the queue directory is unreadable" }

// A successful drain says nothing. The empty queue is the case that runs on
// every camp command, and any output there would be noise on every invocation.
func TestSuccessfulDrainIsSilent(t *testing.T) {
	var out bytes.Buffer
	waited, err := report(&out, Write, jobs.DrainResult{}, nil)
	if err != nil {
		t.Fatalf("report: %v", err)
	}
	if waited != 0 || out.Len() != 0 {
		t.Errorf("a drain with nothing to wait for must be silent; got %q", out.String())
	}
}

// A reporting command says what is queued and returns. This is the half of the
// decision that used to be a wait: `camp status` held the terminal for up to
// DrainTimeout so its answer could be slightly fresher, which is the cost
// deferral exists to remove. The caveat survives, the wait does not.
func TestPendingNoticeSaysWhatIsQueuedAndWhereToLook(t *testing.T) {
	t.Parallel()

	notice := pendingNotice(1)
	if !strings.Contains(notice, "1 queued commit") {
		t.Errorf("notice did not count the work:\n%s", notice)
	}
	if !strings.Contains(notice, "still queued") {
		t.Errorf("notice did not say the report may be incomplete:\n%s", notice)
	}
	if !strings.Contains(notice, "camp jobs") {
		t.Errorf("notice did not name where to look:\n%s", notice)
	}
	// Nothing about waiting: a user told to wait by a command that did not
	// wait learns to distrust both statements.
	for _, forbidden := range []string{"waiting", "after 30s", "timed out"} {
		if strings.Contains(notice, forbidden) {
			t.Errorf("notice mentions %q but nothing waited:\n%s", forbidden, notice)
		}
	}
}

// Singular and plural both have to read correctly, because this line is
// printed constantly and a "1 queued commits" is the kind of wrong that makes
// a user stop reading camp's output.
func TestPendingNoticeAgreesInNumber(t *testing.T) {
	t.Parallel()

	if got := pendingNotice(1); !strings.Contains(got, "1 queued commit ") ||
		!strings.Contains(got, "include it") {
		t.Errorf("pendingNotice(1) = %q, want singular throughout", got)
	}
	if got := pendingNotice(3); !strings.Contains(got, "3 queued commits") ||
		!strings.Contains(got, "include them") {
		t.Errorf("pendingNotice(3) = %q, want plural throughout", got)
	}
}

// An empty queue is silent. A notice on every status in a campaign with
// nothing pending is noise, and noise is how a warning stops being read.
func TestNoticeIsSilentWithAnEmptyQueue(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	n, err := noteAllTo(context.Background(), &out, t.TempDir())
	if err != nil {
		t.Fatalf("noteAllTo() error = %v", err)
	}
	if n != 0 {
		t.Errorf("counted %d outstanding commits in an empty campaign, want 0", n)
	}
	if out.String() != "" {
		t.Errorf("an empty queue printed %q, want silence", out.String())
	}
}

// A cancelled context stops the notice like anything else, rather than doing
// filesystem work nobody is waiting for.
func TestNoticeHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	if _, err := noteAllTo(ctx, &out, t.TempDir()); err == nil {
		t.Error("noteAllTo() with a cancelled context returned nil, want the context error")
	}
}
