package shellcmd

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func requirePowerShell(t *testing.T) {
	t.Helper()
	name := "pwsh"
	if runtime.GOOS == "windows" {
		name = "powershell.exe"
	}
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s unavailable: %v", name, err)
	}
}

func TestPowerShellCommandPreservesLiteralArguments(t *testing.T) {
	requirePowerShell(t)
	want := []string{"two words", "a;b", "$(Write-Output injected)", "O'Brien"}
	for _, tc := range []struct{ name, expression string }{
		{"single-line", "Write-Output"},
		{"YAML-block-scalar", "Write-Output\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := Command(context.Background(), "powershell", tc.expression, want)
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("PowerShell command failed: %v\nstdout:\n%s\nstderr:\n%s", err, out, stderr.String())
			}
			got := strings.Split(strings.TrimSpace(string(out)), "\n")
			for i := range got {
				got[i] = strings.TrimSuffix(got[i], "\r")
			}
			if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
				t.Fatalf("PowerShell argv = %#v, want %#v", got, want)
			}
		})
	}
}

func TestPowerShellCommandSyntaxFailureDoesNotRunPrefix(t *testing.T) {
	requirePowerShell(t)
	cmd, err := Command(context.Background(), "powershell", "Write-Output ('prefix' + 'executed'); if (", nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("invalid PowerShell expression succeeded")
	}
	if strings.Contains(string(out), "prefixexecuted") {
		t.Fatalf("invalid expression executed its prefix: %s", out)
	}
}

func TestCommandRejectsUnknownInterpreter(t *testing.T) {
	_, err := Command(context.Background(), "unknown", "Write-Output ok", nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported command shell") {
		t.Fatalf("unknown interpreter error = %v", err)
	}
}
