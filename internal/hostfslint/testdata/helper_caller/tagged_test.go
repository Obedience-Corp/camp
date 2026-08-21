//go:build container_fs

package helpercaller

import (
	"os/exec"
	"testing"
)

func TestTagged(t *testing.T) {
	_ = exec.Command("git", "status")
	runGit(t, t.TempDir(), "status")
}
