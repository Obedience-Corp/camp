package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	pullsvc "github.com/Obedience-Corp/camp/internal/pull"
)

func TestRunGitPullWithLockRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	output, err := pullsvc.RunGitPullWithLockRetry(ctx, "/path/that/should/not/be/touched", nil, false, pullsvc.IO{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runGitPullWithLockRetry() error = %v, want context.Canceled", err)
	}
	if len(output) != 0 {
		t.Fatalf("runGitPullWithLockRetry() output = %q, want empty output", output)
	}
}

func TestPullNoRebaseHintUsesRelativeProjectPath(t *testing.T) {
	target := &pullsvc.Target{
		Name:    "camp",
		RelPath: "projects/camp",
	}

	got := pullsvc.PullNoRebaseHint(target)
	want := "camp pull --project=projects/camp --no-rebase"
	if got != want {
		t.Fatalf("pullNoRebaseHint() = %q, want %q", got, want)
	}
}

func TestPullNoRebaseHintFallsBackForRoot(t *testing.T) {
	target := &pullsvc.Target{
		Name:   "campaign root",
		IsRoot: true,
	}

	got := pullsvc.PullNoRebaseHint(target)
	want := "camp pull --no-rebase"
	if got != want {
		t.Fatalf("pullNoRebaseHint() = %q, want %q", got, want)
	}
}

func TestPullPushHelpDoesNotAdvertiseProjectShortFlag(t *testing.T) {
	tests := []struct {
		name string
		help string
	}{
		{name: "pull", help: pullCmd.Long},
		{name: "push", help: pushCmd.Long},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, bad := range []string{"--project/-p", " -p projects/"} {
				if strings.Contains(tt.help, bad) {
					t.Fatalf("%s help still advertises project shorthand %q:\n%s", tt.name, bad, tt.help)
				}
			}
			if !strings.Contains(tt.help, "--project=projects/camp") {
				t.Fatalf("%s help does not show --project=projects/camp:\n%s", tt.name, tt.help)
			}
		})
	}
}
