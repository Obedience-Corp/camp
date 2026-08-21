package autowrite

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A writer that never returns must stop being camp's problem.
//
// This is the incident the bound was added for: a writer sat in "generating..."
// for fifty minutes, holding a lane and every drain behind it, while camp
// reported it as ordinary progress. The only fix that ends that wait without a
// human in the loop is a deadline the writer cannot talk its way out of.
func TestWriterDeadline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		command     string
		timeout     time.Duration
		cancelAfter time.Duration
		wantTimeout bool
		wantCancel  bool
		wantMessage string
	}{
		{
			name:        "a writer that never answers is stopped at the bound",
			command:     "sleep 30",
			timeout:     150 * time.Millisecond,
			wantTimeout: true,
		},
		{
			// The reported failure has to name what timed out and how long it
			// was given, because that is the whole content of the decision the
			// user then makes: raise the bound, fix the writer, or drop.
			name:        "a writer that produces output but never exits still times out",
			command:     "printf 'feat: partial\\n'; sleep 30",
			timeout:     150 * time.Millisecond,
			wantTimeout: true,
		},
		{
			name:        "a writer inside the bound is untouched",
			command:     "printf 'feat: quick\\n'",
			timeout:     10 * time.Second,
			wantMessage: "feat: quick",
		},
		{
			// A camp that was told to stop is not a writer that ran out of
			// time, and the worker acts on the difference: a timeout is a
			// verdict on the job and parks it, a shutdown is a verdict on
			// nothing and puts it back.
			name:        "a cancelled run reports cancellation, not a timeout",
			command:     "sleep 30",
			timeout:     10 * time.Second,
			cancelAfter: 100 * time.Millisecond,
			wantCancel:  true,
		},
		{
			name:        "a zero bound leaves the run unbounded",
			command:     "printf 'feat: unbounded\\n'",
			timeout:     0,
			wantMessage: "feat: unbounded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelAfter > 0 {
				timer := time.AfterFunc(tt.cancelAfter, cancel)
				defer timer.Stop()
			}

			start := time.Now()
			message, err := RunCommitMessageCommandWithOptions(ctx, ".", tt.command, RunOptions{
				Timeout:         tt.timeout,
				OwnProcessGroup: true,
			})
			elapsed := time.Since(start)

			switch {
			case tt.wantTimeout:
				var timeoutErr *TimeoutError
				if !errors.As(err, &timeoutErr) {
					t.Fatalf("error = %v, want a *TimeoutError", err)
				}
				if timeoutErr.Command != tt.command {
					t.Errorf("TimeoutError.Command = %q, want %q; the reason has to "+
						"name the writer the user configured", timeoutErr.Command, tt.command)
				}
				if timeoutErr.Timeout != tt.timeout {
					t.Errorf("TimeoutError.Timeout = %v, want %v", timeoutErr.Timeout, tt.timeout)
				}
				if !strings.Contains(err.Error(), tt.command) ||
					!strings.Contains(err.Error(), tt.timeout.String()) {
					t.Errorf("error = %q, want it to name both the writer and the bound", err)
				}
				if message != "" {
					t.Errorf("message = %q, want empty: a message from a writer that "+
						"never finished would be a subject camp invented", message)
				}
				// Generous, but it fails on the regression that matters: a
				// bound that does not actually cut the run short.
				if elapsed > 10*time.Second {
					t.Errorf("the run took %v with a %v bound", elapsed, tt.timeout)
				}
			case tt.wantCancel:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v, want context.Canceled", err)
				}
				var timeoutErr *TimeoutError
				if errors.As(err, &timeoutErr) {
					t.Error("a cancelled run was reported as a timeout; the worker " +
						"would park a job it was supposed to put back")
				}
			default:
				if err != nil {
					t.Fatalf("error = %v, want the writer's message", err)
				}
				if message != tt.wantMessage {
					t.Errorf("message = %q, want %q", message, tt.wantMessage)
				}
			}
		})
	}
}

// The configured bound is read from campaign config, and a value camp cannot
// parse is refused rather than quietly replaced by the default: a typo in a
// timeout must not restore the unbounded writer the field exists to bound.
func TestParseWriterTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    time.Duration
		wantErr string
	}{
		{
			name:    "a value that is not a duration is refused",
			raw:     "5 minutes",
			wantErr: "hooks.commit_message.timeout",
		},
		{
			name:    "zero is refused",
			raw:     "0s",
			wantErr: "must be positive",
		},
		{
			name:    "a negative bound is refused",
			raw:     "-1m",
			wantErr: "must be positive",
		},
		{
			name: "unset takes the shipped default",
			raw:  "",
			want: DefaultWriterTimeout,
		},
		{
			name: "whitespace is not a value",
			raw:  "   ",
			want: DefaultWriterTimeout,
		},
		{
			name: "a configured duration wins",
			raw:  "90s",
			want: 90 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseWriterTimeout(tt.raw)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseWriterTimeout(%q) error = nil, want one naming the field", tt.raw)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseWriterTimeout(%q) error = %v", tt.raw, err)
			}
			if got != tt.want {
				t.Fatalf("parseWriterTimeout(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestDefaultWriterTimeoutNestsAboveObCommitQueueBudget(t *testing.T) {
	t.Parallel()

	// ob/internal/registry.DefaultCommitQueueTimeout; duplicated so camp
	// does not import ob.
	const obCommitQueueBudget = 10 * time.Minute
	if DefaultWriterTimeout <= obCommitQueueBudget {
		t.Fatalf("DefaultWriterTimeout = %v, want > %v so the caller wait nests above ob commit's queue budget",
			DefaultWriterTimeout, obCommitQueueBudget)
	}
	if DefaultWriterTimeout != 12*time.Minute {
		t.Fatalf("DefaultWriterTimeout = %v, want 12m (10m queue budget plus margin)", DefaultWriterTimeout)
	}
}
