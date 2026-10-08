package git

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestDirectionShimBody(t *testing.T) {
	const header = "#!/bin/sh\n# direction-hook v1\n"
	const execLine = `exec fest-direction hook commit-msg "$1"`
	tests := []struct {
		name, body string
		want       bool
	}{
		{"current", header + execLine + "\n", true},
		{"legacy", header + `exec direction hook commit-msg "$@"` + "\n", true},
		{"validation before exec", header + "./validate-message \"$1\" || exit 1\n" + execLine + "\n", false},
		{"command after exec", header + execLine + "\nexit 1\n", false},
		{"exec suffix", header + execLine + "; ./validate-message\n", false},
		{"exec redirect", header + execLine + " > /tmp/message\n", false},
		{"extra argument", header + execLine + " extra\n", false},
		{"wrong message argument", header + strings.ReplaceAll(execLine, "$1", "$2") + "\n", false},
		{"absolute binary", header + strings.ReplaceAll(execLine, "fest-direction", "/custom/fest-direction") + "\n", false},
		{"shell substitution", header + strings.ReplaceAll(execLine, "fest-direction", "$(echo fest-direction)") + "\n", false},
		{"conditional exec", header + "if false; then\n" + execLine + "\nfi\n", false},
		{"different interpreter", strings.ReplaceAll(header, "/bin/sh", "/usr/bin/python") + execLine + "\n", false},
		{"marker only", header, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := directionShimBody(tt.body); got != tt.want {
				t.Fatalf("directionShimBody() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOnlyDirectionShimPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	shim, err := OnlyDirectionShim(ctx, "/unused")
	if shim || !errors.Is(err, context.Canceled) {
		t.Fatalf("OnlyDirectionShim() = %v, %v, want false, canceled", shim, err)
	}
	if !HasCommitHooks(ctx, "/unused") {
		t.Fatal("canceled inspection must refuse deferral")
	}
}
