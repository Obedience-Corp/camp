// Package autowrite runs the configured commit message writer.
//
// It lives in internal/ rather than pkg/commitkit because two callers need it
// and one of them cannot reach commitkit: the deferred-commit worker in
// internal/jobs generates a message for a queued --auto-write commit, and
// commitkit already imports internal/jobs for DrainJobs. Extracting the
// implementation here is what breaks that cycle; pkg/commitkit re-exports it
// unchanged, so the public API is exactly what it was.
package autowrite

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunCommitMessageCommandForwardsDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		command            string
		extraEnv           []string
		wantMessage        string
		wantErr            bool
		wantDiagContains   []string
		wantErrContains    []string
		wantErrNotContains []string
	}{
		{
			name:        "success forwards stderr and returns stdout",
			command:     "printf 'session_id=session-123\\n' >&2; printf 'feat: generated\\n'",
			wantMessage: "feat: generated",
			wantDiagContains: []string{
				"session_id=session-123\n",
			},
		},
		{
			name:        "passes explicit amend contract to writer",
			command:     `test "$CAMP_COMMIT_AMEND" = "1" && printf 'fix: amended\n'`,
			extraEnv:    WithCommitAmendEnv(nil, true),
			wantMessage: "fix: amended",
		},
		{
			// MultiWriter dual-path contract: live forward AND error wrapping.
			// A future refactor must not break either side independently.
			//
			// The two sides carry different amounts on purpose. The live
			// stream is the full diagnostic record; the error is the one line
			// worth repeating next to it, because a writer that fails by
			// printing its usage would otherwise render a screenful twice in
			// the same log.
			name:    "failure forwards all diagnostics but keeps only the reason in the error",
			command: "printf 'ob: generating...\\n' >&2; printf 'hook boom\\n' >&2; exit 1",
			wantErr: true,
			wantDiagContains: []string{
				"ob: generating...",
				"hook boom",
			},
			wantErrContains: []string{
				"hook boom",
				"auto-write commit message command failed",
			},
			wantErrNotContains: []string{
				"ob: generating...",
			},
		},
		{
			// Escape codes must not survive into the error, because the same
			// reason is embedded verbatim in a commit message when a deferred
			// commit degrades.
			name:    "strips ANSI from the reason",
			command: "printf '\\033[1;38;5;230mdaemon not running\\033[0m\\n' >&2; exit 1",
			wantErr: true,
			wantErrContains: []string{
				"daemon not running",
			},
			wantErrNotContains: []string{
				"\x1b[",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var diagnostics bytes.Buffer
			message, err := runCommitMessageCommandWithEnv(
				context.Background(),
				".",
				tt.command,
				tt.extraEnv,
				&diagnostics,
			)

			if tt.wantErr {
				if err == nil {
					t.Fatal("runCommitMessageCommandWithEnv() error = nil, want error")
				}
				if message != "" {
					t.Fatalf("message = %q, want empty on error", message)
				}
				for _, want := range tt.wantErrContains {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("error = %q, want substring %q", err.Error(), want)
					}
				}
				for _, unwanted := range tt.wantErrNotContains {
					if strings.Contains(err.Error(), unwanted) {
						t.Fatalf("error = %q, want it to omit %q", err.Error(), unwanted)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("runCommitMessageCommandWithEnv() error = %v", err)
				}
				if message != tt.wantMessage {
					t.Fatalf("message = %q, want %q", message, tt.wantMessage)
				}
			}

			gotDiag := diagnostics.String()
			for _, want := range tt.wantDiagContains {
				if !strings.Contains(gotDiag, want) {
					t.Fatalf("diagnostics = %q, want substring %q", gotDiag, want)
				}
			}
		})
	}
}

// writerReason is the only part of a failing writer's output that camp repeats
// on the user's behalf: it goes in the error and, when a deferred commit
// degrades, verbatim into a commit message. Everything it must guarantee is
// therefore a guarantee about text that ends up in git history.
func TestWriterReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stderr string
		want   string
	}{
		{
			name:   "empty stderr yields no reason",
			stderr: "",
			want:   "",
		},
		{
			name:   "whitespace only yields no reason",
			stderr: "\n\n   \n",
			want:   "",
		},
		{
			// Tools print their diagnosis after their usage, so the last line
			// is the one that says what went wrong.
			name:   "takes the last meaningful line",
			stderr: "Usage:\n  ob commit [flags]\n\nconnect to daemon: ob: daemon not running\n",
			want:   "connect to daemon: ob: daemon not running",
		},
		{
			name:   "ignores trailing blank lines",
			stderr: "the real reason\n\n\n",
			want:   "the real reason",
		},
		{
			// 256-color sequences, not just the handful of constants camp
			// itself emits: the writer is an arbitrary user-configured tool.
			name:   "strips 256-color escapes",
			stderr: "\x1b[1;38;5;230mdaemon not running\x1b[0m",
			want:   "daemon not running",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := writerReason(tt.stderr); got != tt.want {
				t.Fatalf("writerReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A writer that dumps an unbounded blob must not put an unbounded blob in a
// commit message.
func TestWriterReasonIsBounded(t *testing.T) {
	t.Parallel()

	reason := writerReason(strings.Repeat("x", maxWriterReasonBytes*3))
	if len(reason) > maxWriterReasonBytes+3 {
		t.Fatalf("writerReason() length = %d, want at most %d",
			len(reason), maxWriterReasonBytes+3)
	}
	if !strings.HasSuffix(reason, "...") {
		t.Fatalf("writerReason() = %q, want a truncation marker", reason)
	}
}
