//go:build integration && dev

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_ExecuteFresh_CreatesAndPushesNewBranch(t *testing.T) {
	campDir, _ := setupCampaignWithSubmodule(t)
	subDir := filepath.Join(campDir, "projects", "test-project")

	err := executeFresh(context.Background(), "test-project", subDir, freshOptions{
		branch:      "feat/new-work",
		prune:       true,
		pruneRemote: true,
		push:        true,
	})
	if err != nil {
		t.Fatalf("executeFresh() error = %v", err)
	}

	current := strings.TrimSpace(run(t, "git", "-C", subDir, "rev-parse", "--abbrev-ref", "HEAD"))
	if current != "feat/new-work" {
		t.Fatalf("current branch = %q, want %q", current, "feat/new-work")
	}

	upstream := strings.TrimSpace(run(t, "git", "-C", subDir, "rev-parse", "--abbrev-ref", "@{upstream}"))
	if upstream != "origin/feat/new-work" {
		t.Fatalf("upstream = %q, want %q", upstream, "origin/feat/new-work")
	}
}

func TestIntegration_ExecuteFresh_DoesNotPushExistingBranch(t *testing.T) {
	campDir, _ := setupCampaignWithSubmodule(t)
	subDir := filepath.Join(campDir, "projects", "test-project")

	run(t, "git", "-C", subDir, "checkout", "-b", "develop")
	if err := os.WriteFile(filepath.Join(subDir, "develop.txt"), []byte("develop"), 0o644); err != nil {
		t.Fatalf("failed to write develop.txt: %v", err)
	}
	run(t, "git", "-C", subDir, "add", ".")
	run(t, "git", "-C", subDir, "commit", "-m", "Develop work")
	run(t, "git", "-C", subDir, "checkout", "main")

	err := executeFresh(context.Background(), "test-project", subDir, freshOptions{
		branch:      "develop",
		prune:       true,
		pruneRemote: true,
		push:        true,
	})
	if err != nil {
		t.Fatalf("executeFresh() error = %v", err)
	}

	current := strings.TrimSpace(run(t, "git", "-C", subDir, "rev-parse", "--abbrev-ref", "HEAD"))
	if current != "main" {
		t.Fatalf("current branch = %q, want %q", current, "main")
	}

	cmd := exec.Command("git", "-C", subDir, "rev-parse", "--abbrev-ref", "develop@{upstream}")
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("expected existing branch to remain without upstream, got %s", strings.TrimSpace(string(output)))
	}
}
