//go:build !dev

package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestReleaseProfileStable_GendocsCommandNotRegistered(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "gendocs" {
			t.Fatal("gendocs should not be registered in stable profile")
		}
	}
}

func TestReleaseProfileStable_CobraDocNotInDeps(t *testing.T) {
	// Verify the stable build graph does not pull cobra/doc, confirming
	// the dev-only implementation is truly excluded at compile time.
	out, err := exec.Command("go", "list", "-deps", "github.com/Obedience-Corp/camp/cmd/camp").CombinedOutput()
	if err != nil {
		t.Skipf("go list failed: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "github.com/spf13/cobra/doc" {
			t.Fatal("stable build should not depend on github.com/spf13/cobra/doc; gendocs is leaking into the stable binary")
		}
	}
}
