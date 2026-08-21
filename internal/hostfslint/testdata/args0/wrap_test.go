package args0

import (
	"os/exec"
	"testing"
)

func mustRunCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

func TestInitViaArgs0(t *testing.T) {
	mustRunCmd(t, t.TempDir(), "git", "init")
}
