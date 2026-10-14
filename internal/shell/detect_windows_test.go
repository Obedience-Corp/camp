//go:build windows

package shell

import "testing"

func TestInitHintDefaultsToPowerShellOnWindows(t *testing.T) {
	t.Setenv("SHELL", "")
	if got, want := InitHint(), "Invoke-Expression (& camp shell-init powershell | Out-String)"; got != want {
		t.Fatalf("InitHint() = %q, want %q", got, want)
	}
}
