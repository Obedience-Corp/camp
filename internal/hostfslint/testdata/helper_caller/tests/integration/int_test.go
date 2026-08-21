package integration

import (
	"os/exec"
	"testing"
)

func TestIntegrationGit(t *testing.T) {
	_ = exec.Command("git", "status")
}
