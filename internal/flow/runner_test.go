package flow

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewRunner(t *testing.T) {
	r := NewRunner("/some/root")
	if r.campaignRoot != "/some/root" {
		t.Errorf("campaignRoot = %q, want %q", r.campaignRoot, "/some/root")
	}
}

func TestRunner_resolveWorkDir(t *testing.T) {
	r := NewRunner("/campaign")

	tests := []struct {
		name    string
		workDir string
		want    string
	}{
		{"empty", "", "/campaign"},
		{"dot", ".", "/campaign"},
		{"relative", "projects/camp", "/campaign/projects/camp"},
		{"absolute", "/absolute/path", "/absolute/path"},
		{"nested relative", "a/b/c", "/campaign/a/b/c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.resolveWorkDir(tt.workDir)
			if got != tt.want {
				t.Errorf("resolveWorkDir(%q) = %q, want %q", tt.workDir, got, tt.want)
			}
		})
	}
}

func TestRunner_mergeEnv(t *testing.T) {
	r := NewRunner("/campaign")

	t.Run("nil flow env returns os environ", func(t *testing.T) {
		result := r.mergeEnv(nil)
		if len(result) == 0 {
			t.Error("expected non-empty env from os.Environ()")
		}
	})

	t.Run("empty flow env returns os environ", func(t *testing.T) {
		result := r.mergeEnv(map[string]string{})
		if len(result) == 0 {
			t.Error("expected non-empty env from os.Environ()")
		}
	})

	t.Run("flow env overrides existing", func(t *testing.T) {
		flowEnv := map[string]string{
			"TEST_FLOW_VAR": "flow_value",
		}
		result := r.mergeEnv(flowEnv)

		found := false
		for _, entry := range result {
			if entry == "TEST_FLOW_VAR=flow_value" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected TEST_FLOW_VAR=flow_value in merged env")
		}
	})
}

func TestRunner_Run_Success(t *testing.T) {
	tmp := t.TempDir()
	r := NewRunner(tmp)

	outFile := filepath.Join(tmp, "output.txt")
	f := Flow{
		Command: "echo hello > " + outFile,
	}

	ctx := context.Background()
	if err := r.Run(ctx, f, nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	if got := string(data); got != "hello\n" {
		t.Errorf("output = %q, want %q", got, "hello\n")
	}
}

func TestRunner_Run_ExtraArgPositionals(t *testing.T) {
	tests := []struct {
		name      string
		extraArgs []string
		want      []string
	}{
		{
			name:      "space remains one arg",
			extraArgs: []string{"us east"},
			want:      []string{"us east"},
		},
		{
			name:      "command substitution is data",
			extraArgs: []string{"$(echo injected)"},
			want:      []string{"$(echo injected)"},
		},
		{
			name:      "semicolon is data",
			extraArgs: []string{"a;b"},
			want:      []string{"a;b"},
		},
		{
			name:      "pipe is data",
			extraArgs: []string{"a|b"},
			want:      []string{"a|b"},
		},
		{
			name:      "multiple args retain boundaries",
			extraArgs: []string{"one two", "--flag", "-v", "*.go"},
			want:      []string{"one two", "--flag", "-v", "*.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			r := NewRunner(tmp)

			outFile := filepath.Join(tmp, "args.txt")
			f := Flow{
				Command: "printf '%s\n' > \"$OUT_FILE\"",
				Env:     map[string]string{"OUT_FILE": outFile},
			}

			if err := r.Run(context.Background(), f, tt.extraArgs); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			data, err := os.ReadFile(outFile)
			if err != nil {
				t.Fatalf("reading output file: %v", err)
			}
			got := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if len(tt.want) == 0 && len(got) == 1 && got[0] == "" {
				got = nil
			}
			if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
				t.Errorf("output args = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRunner_Run_EmptyExtraArgs(t *testing.T) {
	tmp := t.TempDir()
	r := NewRunner(tmp)

	outFile := filepath.Join(tmp, "empty.txt")
	f := Flow{
		Command: "printf 'hello\n' > \"$OUT_FILE\"",
		Env:     map[string]string{"OUT_FILE": outFile},
	}

	if err := r.Run(context.Background(), f, nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	if got := string(data); got != "hello\n" {
		t.Errorf("output = %q, want %q", got, "hello\n")
	}
}

func TestRunner_Run_WithWorkDir(t *testing.T) {
	tmp := t.TempDir()
	subDir := filepath.Join(tmp, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	outFile := filepath.Join(tmp, "pwd.txt")
	r := NewRunner(tmp)
	f := Flow{
		Command: "pwd > " + outFile,
		WorkDir: "subdir",
	}

	ctx := context.Background()
	if err := r.Run(ctx, f, nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}

	// Resolve symlinks for macOS /var -> /private/var
	expected, _ := filepath.EvalSymlinks(subDir)
	got := string(data)
	got = got[:len(got)-1] // strip newline
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != expected {
		t.Errorf("workdir = %q, want %q", gotResolved, expected)
	}
}

func TestRunner_Run_WithEnv(t *testing.T) {
	tmp := t.TempDir()
	r := NewRunner(tmp)

	outFile := filepath.Join(tmp, "env.txt")
	f := Flow{
		Command: "echo $MY_FLOW_VAR > " + outFile,
		Env:     map[string]string{"MY_FLOW_VAR": "hello_flow"},
	}

	ctx := context.Background()
	if err := r.Run(ctx, f, nil); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	if got := string(data); got != "hello_flow\n" {
		t.Errorf("env output = %q, want %q", got, "hello_flow\n")
	}
}

func TestRunner_Run_FailingCommand(t *testing.T) {
	r := NewRunner(t.TempDir())
	f := Flow{
		Command: "exit 7",
	}

	ctx := context.Background()
	err := r.Run(ctx, f, nil)
	if err == nil {
		t.Error("expected error for failing command")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Run() error = %T %v, want *exec.ExitError in chain", err, err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code = %d, want 7", exitErr.ExitCode())
	}
}

func TestRunner_Run_CancelledContext(t *testing.T) {
	r := NewRunner(t.TempDir())
	f := Flow{
		Command: "echo hello",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := r.Run(ctx, f, nil); err == nil {
		t.Error("expected error for cancelled context")
	}
}
