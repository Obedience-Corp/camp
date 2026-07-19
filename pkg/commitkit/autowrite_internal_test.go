package commitkit

import (
	"bytes"
	"context"
	"testing"
)

func TestRunCommitMessageCommandForwardsDiagnostics(t *testing.T) {
	var diagnostics bytes.Buffer

	message, err := runCommitMessageCommandWithEnv(
		context.Background(),
		t.TempDir(),
		"printf 'session_id=session-123\\n' >&2; printf 'feat: generated\\n'",
		nil,
		&diagnostics,
	)
	if err != nil {
		t.Fatalf("runCommitMessageCommandWithEnv() error = %v", err)
	}
	if message != "feat: generated" {
		t.Fatalf("message = %q, want %q", message, "feat: generated")
	}
	if got, want := diagnostics.String(), "session_id=session-123\n"; got != want {
		t.Fatalf("diagnostics = %q, want %q", got, want)
	}
}
