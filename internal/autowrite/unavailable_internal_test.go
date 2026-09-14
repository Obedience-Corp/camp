package autowrite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The exit-75 contract is the whole of what a writer can say about its own
// availability, and camp spends a user's commit message on the answer. These
// assert the classification itself, on real processes, because the thing being
// claimed is about exit statuses rather than about camp's own types.
func TestWriterUnavailableClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		command         string
		wantUnavailable bool
		wantReason      string
	}{
		{
			name:            "exit 75 is temporarily unavailable",
			command:         "printf 'ob: daemon not running\\n' >&2; exit 75",
			wantUnavailable: true,
			wantReason:      "ob: daemon not running",
		},
		{
			// The everyday failure. Nothing about it says the writer would
			// answer if asked again, so nothing about it may delay a commit.
			name:            "exit 1 is an ordinary failure",
			command:         "printf 'hook boom\\n' >&2; exit 1",
			wantUnavailable: false,
			wantReason:      "hook boom",
		},
		{
			name:            "a command that does not exist is not unavailable",
			command:         "camp-no-such-writer-exists",
			wantUnavailable: false,
		},
		{
			// Adjacent statuses must not drift into the contract: 74 and 76
			// are EX_IOERR and EX_PROTOCOL, which are the writer telling camp
			// it is broken rather than busy.
			name:            "a neighbouring sysexits status is not unavailable",
			command:         "exit 76",
			wantUnavailable: false,
		},
		{
			// Exit 0 with no message is already folded into the failure
			// vocabulary, and it is a writer that answered: asking it again
			// would produce the same nothing.
			name:            "a silent success is a failure but not unavailable",
			command:         "exit 0",
			wantUnavailable: false,
			wantReason:      "the writer exited cleanly without printing a message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			message, err := RunCommitMessageCommand(context.Background(), ".", tt.command)
			if err == nil {
				t.Fatalf("RunCommitMessageCommand() = %q, want an error", message)
			}
			if got := WriterUnavailable(err); got != tt.wantUnavailable {
				t.Fatalf("WriterUnavailable(%v) = %v, want %v", err, got, tt.wantUnavailable)
			}
			var writerErr *WriterError
			if !errors.As(err, &writerErr) {
				t.Fatalf("error = %T, want a *WriterError carrying the writer's own reason", err)
			}
			if tt.wantReason != "" && writerErr.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", writerErr.Reason, tt.wantReason)
			}
		})
	}
}

// An unavailable writer's error is read by a person in worker.log and in
// `camp jobs`, so it has to say the thing that is true: nobody answered.
func TestWriterUnavailableErrorText(t *testing.T) {
	t.Parallel()

	err := &WriterError{Command: "ob commit", Reason: "daemon not running", Unavailable: true}
	if !strings.Contains(err.Error(), "temporarily unavailable") {
		t.Fatalf("Error() = %q, want it to say the writer was unavailable", err.Error())
	}
	if !strings.Contains(err.Error(), "daemon not running") {
		t.Fatalf("Error() = %q, want the writer's own reason", err.Error())
	}
	failed := &WriterError{Command: "ob commit", Reason: "daemon not running"}
	if strings.Contains(failed.Error(), "temporarily unavailable") {
		t.Fatalf("Error() = %q, want an ordinary failure to keep saying it failed", failed.Error())
	}
}

// A nil and a non-writer error must not read as "ask again later": everything
// that is not the contract lands the commit now.
func TestWriterUnavailableRejectsOtherErrors(t *testing.T) {
	t.Parallel()

	if WriterUnavailable(nil) {
		t.Fatal("WriterUnavailable(nil) = true")
	}
	if WriterUnavailable(ErrCommitMessageHookEmptyOutput) {
		t.Fatal("WriterUnavailable(empty output) = true")
	}
	if WriterUnavailable(&TimeoutError{Command: "ob commit", Timeout: time.Minute}) {
		t.Fatal("WriterUnavailable(timeout) = true")
	}
}

// The window is how long a user's commit can sit unlanded, so a value camp
// cannot read is refused rather than guessed at — the same call the timeout
// makes. Zero is the exception, because it is the only way to spell the old
// behavior.
func TestParseRetryWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr bool
	}{
		{name: "unset takes the default", raw: "", want: DefaultRetryWindow},
		{name: "a duration is honored", raw: "10m", want: 10 * time.Minute},
		{name: "zero disables the wait", raw: "0", want: 0},
		{name: "zero with a unit disables the wait", raw: "0s", want: 0},
		{name: "surrounding space is tolerated", raw: "  45s  ", want: 45 * time.Second},
		{name: "nonsense is refused", raw: "soon", wantErr: true},
		{name: "negative is refused", raw: "-1h", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseRetryWindow(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseRetryWindow(%q) = %v, want an error", tt.raw, got)
				}
				if !strings.Contains(err.Error(), "retry_window") {
					t.Fatalf("error = %q, want it to name the field the user has to fix", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRetryWindow(%q) error = %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("parseRetryWindow(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
