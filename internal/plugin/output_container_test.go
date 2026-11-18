//go:build container_fs && !windows

package plugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func writeScript(t *testing.T, name, body string) Plugin {
	t.Helper()
	bin := filepath.Join(t.TempDir(), Prefix+name)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return Plugin{Name: name, Path: bin}
}

func TestOutput_DropsCampRootAndTerminalInput(t *testing.T) {
	t.Setenv(EnvCampRoot, "/some/camp")
	p := writeScript(t, "env", `echo "root=${CAMP_ROOT-unset}"; if read -r line; then echo "stdin=$line"; else echo "stdin=eof"; fi; echo noise >&2`)
	out, err := Output(t.Context(), p, nil, 1024)
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	if got := string(out); got != "root=unset\nstdin=eof\n" {
		t.Fatalf("Output() = %q, want CAMP_ROOT unset, no input and no stderr", got)
	}
}

func TestOutput_NonZeroExitKeepsStdout(t *testing.T) {
	p := writeScript(t, "refuse", `printf '{"ok":false}'; exit 2`)
	out, err := Output(t.Context(), p, []string{"skill", "--json"}, 1024)
	var cmdErr *camperrors.CommandError
	if !errors.As(err, &cmdErr) || cmdErr.ExitCode != 2 || string(out) != `{"ok":false}` {
		t.Fatalf("Output() = %q, %v; want the refusal and exit 2", out, err)
	}
}

func TestOutput_CapStopsThePlugin(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "pid")
	p := writeScript(t, "flood", `echo $$ > `+shellQuote(ready)+`; while :; do echo xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; done`)
	prev := pluginCancelGrace
	pluginCancelGrace = 200 * time.Millisecond
	t.Cleanup(func() { pluginCancelGrace = prev })

	start := time.Now()
	out, err := Output(t.Context(), p, nil, 4096)
	if !errors.Is(err, ErrOutputTooLarge) || len(out) != 4096 {
		t.Fatalf("Output() = %d bytes, %v; want 4096 and ErrOutputTooLarge", len(out), err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Output() took %s to stop a flooding plugin", elapsed)
	}
	pid := waitForPIDFile(t, ready, time.Second)
	deadline := time.Now().Add(time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("flooding plugin %d still alive", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOutput_DeadlineInterruptsThenKills(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "pid")
	p := writeScript(t, "wedged", "trap '' INT TERM\necho $$ > "+shellQuote(ready)+"\nwhile :; do sleep 0.1; done\n")
	prev := pluginCancelGrace
	pluginCancelGrace = 200 * time.Millisecond
	t.Cleanup(func() { pluginCancelGrace = prev })

	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Output(ctx, p, nil, 1024)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Output() error = %v, want the deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Output() returned after %s; want deadline plus grace", elapsed)
	}
	pid := waitForPIDFile(t, ready, time.Second)
	if processAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("wedged plugin %d outlived the grace period", pid)
	}
}

func TestOutput_InterruptLetsThePluginFinish(t *testing.T) {
	p := writeScript(t, "polite", "trap 'echo interrupted; exit 130' INT\nwhile :; do sleep 0.05; done\n")
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	out, err := Output(ctx, p, nil, 1024)
	if !errors.Is(err, context.Canceled) || !strings.Contains(string(out), "interrupted") {
		t.Fatalf("Output() = %q, %v; want the plugin's own finish and context.Canceled", out, err)
	}
}

func TestOutput_RunsOutsideTheCampWithoutPWD(t *testing.T) {
	camp := t.TempDir()
	t.Chdir(camp)
	t.Setenv("PWD", camp)
	t.Setenv(EnvCampRoot, camp)

	env, err := Output(t.Context(), Plugin{Name: "env", Path: "/usr/bin/env"}, nil, 1<<16)
	if err != nil {
		t.Fatalf("Output(env) error = %v", err)
	}
	for _, line := range strings.Split(string(env), "\n") {
		if strings.HasPrefix(line, "PWD=") || strings.HasPrefix(line, EnvCampRoot+"=") {
			t.Fatalf("plugin environment holds %q", line)
		}
	}
	wd, err := Output(t.Context(), Plugin{Name: "pwd", Path: "/bin/pwd"}, nil, 1024)
	if err != nil {
		t.Fatalf("Output(pwd) error = %v", err)
	}
	want, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(wd)); got != want || strings.HasPrefix(got, camp) {
		t.Fatalf("plugin ran in %q, want the temporary directory %q, never the camp", got, want)
	}
}

func TestOutput_ChildHoldingStdoutAfterSuccessKeepsTheAnswer(t *testing.T) {
	prev := pluginCancelGrace
	pluginCancelGrace = 200 * time.Millisecond
	t.Cleanup(func() { pluginCancelGrace = prev })
	p := writeScript(t, "lingering", "echo answer\nsleep 2 &\nexit 0\n")
	out, err := Output(t.Context(), p, nil, 1024)
	if err != nil || string(out) != "answer\n" {
		t.Fatalf("Output() = %q, %v; want the answer and no error", out, err)
	}
}
