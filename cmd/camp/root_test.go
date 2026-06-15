package main

import (
	"context"
	"errors"
	"os"
	"testing"

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
