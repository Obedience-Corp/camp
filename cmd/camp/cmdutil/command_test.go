package cmdutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestExecuteDirectPreservesArgv(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	outFile := filepath.Join(dir, "args.out")
	helper := filepath.Join(binDir, "just")
	script := `#!/bin/sh
printf '%s\n' "$CAMP_ROOT" > "$ARG_OUT"
for arg do
	printf '%s\n' "$arg" >> "$ARG_OUT"
done
`
	if err := os.WriteFile(helper, []byte(script), 0755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "match.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("ARG_OUT", outFile)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	args := []string{
		"recipe",
		"fix: two words",
		"$HOME",
		"a;b",
		"*.go",
		"--",
		"-flag",
		`say "hi"`,
	}
	const campaignRoot = "/tmp/campaign-root"
	if err := ExecuteDirect(context.Background(), "just", args, dir, campaignRoot); err != nil {
		t.Fatalf("ExecuteDirect() error = %v", err)
	}

	gotBytes, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	got := strings.Split(strings.TrimSuffix(string(gotBytes), "\n"), "\n")
	want := append([]string{campaignRoot}, args...)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExecuteDirect() argv = %#v, want %#v", got, want)
	}
}

func TestExecuteDirectPropagatesExitCode(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	helper := filepath.Join(binDir, "just")
	script := `#!/bin/sh
exit 37
`
	if err := os.WriteFile(helper, []byte(script), 0755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := ExecuteDirect(context.Background(), "just", []string{"fail"}, dir, "")
	if err == nil {
		t.Fatal("ExecuteDirect() error = nil, want command error")
	}

	var cmdErr *camperrors.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("ExecuteDirect() error = %T, want *CommandError", err)
	}
	if cmdErr.ExitCode != 37 {
		t.Fatalf("CommandError.ExitCode = %d, want 37", cmdErr.ExitCode)
	}
	if !strings.Contains(cmdErr.Command, "just fail") {
		t.Fatalf("CommandError.Command = %q, want just fail", cmdErr.Command)
	}
}

func TestExecuteDirectCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ExecuteDirect(ctx, "just", nil, t.TempDir(), "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExecuteDirect() error = %v, want context.Canceled", err)
	}
}
