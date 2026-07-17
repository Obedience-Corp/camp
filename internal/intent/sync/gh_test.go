package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNewGHChecker_NotFoundOnPATH(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	_, err := NewGHChecker()
	if !errors.Is(err, ErrGHNotFound) {
		t.Fatalf("NewGHChecker() error = %v, want ErrGHNotFound", err)
	}
}

// writeFakeGH writes an executable shell script named gh into dir that
// answers `gh pr view <url> --json state` from a fixed table of url -> JSON
// response, or a nonzero exit with stderr for urls not in the table. This
// exercises the real GHChecker.State parsing/error-classification path
// against a real subprocess, without depending on network access or a real
// GitHub PR existing.
func writeFakeGH(t *testing.T, responses map[string]string, failures map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	script := "#!/bin/sh\ncase \"$3\" in\n"
	for url, body := range responses {
		script += "  '" + url + "') echo '" + body + "'; exit 0 ;;\n"
	}
	for url, stderr := range failures {
		script += "  '" + url + "') echo '" + stderr + "' >&2; exit 1 ;;\n"
	}
	script += "  *) echo \"fake gh: no fixture for $3\" >&2; exit 1 ;;\nesac\n"

	path := filepath.Join(dir, "gh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake gh script: %v", err)
	}
	return dir
}

func TestGHChecker_State(t *testing.T) {
	const mergedURL = "https://github.com/o/r/pull/1"
	const closedURL = "https://github.com/o/r/pull/2"
	const openURL = "https://github.com/o/r/pull/3"
	const missingURL = "https://github.com/o/r/pull/404"

	dir := writeFakeGH(t, map[string]string{
		mergedURL: `{"state":"MERGED"}`,
		closedURL: `{"state":"CLOSED"}`,
		openURL:   `{"state":"OPEN"}`,
	}, map[string]string{
		missingURL: "GraphQL: Could not resolve to a PullRequest",
	})
	t.Setenv("PATH", dir)

	checker, err := NewGHChecker()
	if err != nil {
		t.Fatalf("NewGHChecker() error = %v", err)
	}

	tests := []struct {
		name    string
		url     string
		want    PRState
		wantErr bool
	}{
		{name: "merged", url: mergedURL, want: PRStateMerged},
		{name: "closed", url: closedURL, want: PRStateClosed},
		{name: "open", url: openURL, want: PRStateOpen},
		{name: "gh query failure surfaces stderr", url: missingURL, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := checker.State(context.Background(), tt.url)
			if (err != nil) != tt.wantErr {
				t.Fatalf("State(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
			if tt.wantErr {
				if !errors.Is(err, ErrGHQueryFailed) {
					t.Fatalf("State(%q) error = %v, want wrapped ErrGHQueryFailed", tt.url, err)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("State(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestGHChecker_State_ContextCancelled(t *testing.T) {
	dir := writeFakeGH(t, map[string]string{"https://github.com/o/r/pull/1": `{"state":"OPEN"}`}, nil)
	t.Setenv("PATH", dir)

	checker, err := NewGHChecker()
	if err != nil {
		t.Fatalf("NewGHChecker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := checker.State(ctx, "https://github.com/o/r/pull/1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("State() with cancelled context error = %v, want context.Canceled", err)
	}
}
