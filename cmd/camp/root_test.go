package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
)

func TestExecuteContext_CancelledContext(t *testing.T) {
	cmd := &cobra.Command{
		Use: "ctx-check",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Context().Err()
		},
	}
	rootCmd.AddCommand(cmd)
	t.Cleanup(func() {
		rootCmd.RemoveCommand(cmd)
		rootCmd.SetArgs(nil)
	})

	oldArgs := os.Args
	os.Args = []string{"camp", "ctx-check"}
	t.Cleanup(func() { os.Args = oldArgs })

	rootCmd.SetArgs([]string{"ctx-check"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := Execute(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(ctx) error = %v, want context.Canceled", err)
	}
}

func TestNoAliasCollisions(t *testing.T) {
	seen := map[string]string{}
	for _, cmd := range rootCmd.Commands() {
		for _, alias := range cmd.Aliases {
			if prev, ok := seen[alias]; ok {
				t.Errorf("alias %q is used by both %q and %q", alias, prev, cmd.Name())
			}
			seen[alias] = cmd.Name()
		}
	}
}

func TestRegistryAliasRoutesToRegistry(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"reg"})
	if err != nil {
		t.Fatalf("rootCmd.Find(reg) error = %v", err)
	}
	if cmd == nil || cmd.Name() != "registry" {
		t.Fatalf("rootCmd.Find(reg) = %#v, want registry command", cmd)
	}
}

func TestRuntimeErrorNoUsageDump(t *testing.T) {
	root := makeTestCampaign(t, "usage-runtime")
	if err := os.MkdirAll(filepath.Join(root, ".campaign", "intents", "inbox"), 0755); err != nil {
		t.Fatalf("mkdir intents: %v", err)
	}
	chdirForTest(t, root)

	stdout, stderr, err := executeRootUsageTestCommand(t, "intent", "show", "does-not-exist-xyz")
	if err == nil {
		t.Fatal("Execute() error = nil, want runtime error")
	}
	combined := stdout + stderr
	if strings.Contains(combined, "Usage:") || strings.Contains(combined, "Flags:") {
		t.Fatalf("output contains usage text for runtime error:\n%s", combined)
	}
}

func TestFlagParseErrorShowsUsage(t *testing.T) {
	stdout, stderr, err := executeRootUsageTestCommand(t, "intent", "show", "--bad-flag")
	if err == nil {
		t.Fatal("Execute() error = nil, want flag parse error")
	}
	var cmdErr *camperrors.CommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("Execute() error = %T %v, want *CommandError", err, err)
	}
	if cmdErr.ExitCode != 2 {
		t.Fatalf("CommandError.ExitCode = %d, want 2", cmdErr.ExitCode)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "Usage:") {
		t.Fatalf("output = %q, want usage text", combined)
	}
}

func executeRootUsageTestCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer
	rootCmd.SetArgs(args)
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SilenceUsage = true
	})

	oldArgs := os.Args
	os.Args = append([]string{"camp"}, args...)
	t.Cleanup(func() { os.Args = oldArgs })

	err := Execute(context.Background())
	return stdout.String(), stderr.String(), err
}
