package commitkit

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunCommitMessageCommandForwardsDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		command          string
		extraEnv         []string
		wantMessage      string
		wantErr          bool
		wantDiagContains []string
		wantErrContains  []string
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
			name:    "failure forwards diagnostics and keeps stderr in wrapped error",
			command: "printf 'ob: generating...\\n' >&2; printf 'hook boom\\n' >&2; exit 1",
			wantErr: true,
			wantDiagContains: []string{
				"ob: generating...",
				"hook boom",
			},
			wantErrContains: []string{
				"ob: generating...",
				"hook boom",
				"auto-write commit message command failed",
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
