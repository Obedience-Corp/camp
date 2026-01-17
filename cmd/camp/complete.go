package main

import (
	"fmt"

	"github.com/obediencecorp/camp/internal/complete"
	"github.com/spf13/cobra"
)

var completeCmd = &cobra.Command{
	Use:    "complete [args...]",
	Short:  "Generate completion candidates",
	Hidden: true, // Not shown in help, for shell integration only
	RunE:   runComplete,
}

func init() {
	rootCmd.AddCommand(completeCmd)
}

func runComplete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	candidates, err := complete.Generate(ctx, args)
	if err != nil {
		// Silent failure - don't pollute completion output
		return nil
	}

	for _, c := range candidates {
		fmt.Println(c)
	}
	return nil
}
