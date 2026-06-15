package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/spf13/cobra"
)

func TestStatusCommandRegistersRealFlags(t *testing.T) {
	for _, name := range []string{"sub", "project", "short", "show-refs"} {
		if statusCmd.Flags().Lookup(name) == nil {
			t.Fatalf("status flag %q is not registered", name)
		}
	}
	if got := statusCmd.Flags().ShorthandLookup("p"); got == nil || got.Name != "project" {
		t.Fatalf("-p shorthand = %#v, want project", got)
	}
	if got := statusCmd.Flags().ShorthandLookup("s"); got == nil || got.Name != "short" {
		t.Fatalf("-s shorthand = %#v, want short", got)
	}
	if statusCmd.DisableFlagParsing {
		t.Fatal("status command should use cobra flag parsing")
	}
}

func TestExtractShowRefs_NotPresent(t *testing.T) {
	args := []string{"--short", "-s"}
	filtered, showRefs := extractShowRefs(args)

	if showRefs {
		t.Error("showRefs should be false when --show-refs not present")
	}
	if len(filtered) != 2 {
		t.Errorf("filtered args length = %d, want 2", len(filtered))
	}
}

func TestExtractShowRefs_Present(t *testing.T) {
	args := []string{"--short", "--show-refs", "-s"}
	filtered, showRefs := extractShowRefs(args)

	if !showRefs {
		t.Error("showRefs should be true when --show-refs present")
	}
	if len(filtered) != 2 {
		t.Errorf("filtered args length = %d, want 2", len(filtered))
	}
	for _, arg := range filtered {
		if arg == "--show-refs" {
			t.Error("--show-refs should be removed from filtered args")
		}
	}
}

func TestExtractShowRefs_OnlyShowRefs(t *testing.T) {
	args := []string{"--show-refs"}
	filtered, showRefs := extractShowRefs(args)

	if !showRefs {
		t.Error("showRefs should be true")
	}
	if len(filtered) != 0 {
		t.Errorf("filtered args length = %d, want 0", len(filtered))
	}
}

func TestExtractShowRefs_Empty(t *testing.T) {
	args := []string{}
	filtered, showRefs := extractShowRefs(args)

	if showRefs {
		t.Error("showRefs should be false for empty args")
	}
	if len(filtered) != 0 {
		t.Errorf("filtered args length = %d, want 0", len(filtered))
	}
}

func TestRunStatusWrapsGitFailureWithTargetPath(t *testing.T) {
	root := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	t.Setenv(campaign.EnvCacheDisable, "1")

	statusSub = false
	statusProject = ""
	statusShort = false
	statusShowRefs = false

	cmd := &cobra.Command{Use: "status"}
	cmd.SetContext(context.Background())
	err = runStatus(cmd, nil)
	if err == nil {
		t.Fatal("runStatus() error = nil, want git status failure")
	}
	if !strings.Contains(err.Error(), "git status failed for "+resolvedRoot) {
		t.Fatalf("runStatus() error = %q, want target path %q", err.Error(), resolvedRoot)
	}
}
