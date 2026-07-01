package main

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
)

func TestWorktreesExcludeSpec_FallsBackToDefaultOutsideCampaign(t *testing.T) {
	got := worktreesExcludeSpec(context.Background(), t.TempDir())
	if got != "projects/worktrees" {
		t.Fatalf("worktreesExcludeSpec() = %q, want %q (default, no trailing slash)", got, "projects/worktrees")
	}
}

func TestEffectiveCommitAll_AmendDefaultsNoAutoStage(t *testing.T) {
	cmd := &cobra.Command{Use: "commit"}
	var all bool
	cmd.Flags().BoolVarP(&all, "all", "a", true, "")

	if got := effectiveCommitAll(cmd, true, true); got {
		t.Fatal("amend without explicit --all should not auto-stage")
	}
}

func TestEffectiveCommitAll_AmendHonorsExplicitAll(t *testing.T) {
	cmd := &cobra.Command{Use: "commit"}
	var all bool
	cmd.Flags().BoolVarP(&all, "all", "a", true, "")
	if err := cmd.ParseFlags([]string{"--all"}); err != nil {
		t.Fatal(err)
	}

	if got := effectiveCommitAll(cmd, true, all); !got {
		t.Fatal("amend with explicit --all should auto-stage")
	}
}

