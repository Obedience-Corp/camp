package drain

import (
	"bytes"
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
	for _, mode := range []Mode{Write, Commit} {
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
	), "Pushing")

	for _, want := range []string{
		"capture intent: one",
		"capture intent: two",
		"camp jobs",
		"--no-drain",
		"camp jobs drop",
		"Pushing now would leave them behind",
	} {
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
}

// A non-timeout error passes through untouched. Turning a real failure into a
// warning would hide it behind copy written for a slow queue.
func TestNonTimeoutErrorsAreNotSwallowed(t *testing.T) {
	var out bytes.Buffer
	boom := errBoom{}
	for _, mode := range []Mode{Read, Write, Commit} {
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
