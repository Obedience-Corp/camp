package main

import (
	"testing"

	"github.com/spf13/cobra"
)

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

