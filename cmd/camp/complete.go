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
	completeCmd.GroupID = "system"
	completeCmd.Flags().Bool("described", false, "Output name and path descriptions (tab-separated)")
}

func runComplete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	described, _ := cmd.Flags().GetBool("described")

	if described {
		categories, err := complete.GenerateRich(ctx, args)
		if err != nil {
			// Silent failure - don't pollute completion output
			return nil
		}
		for _, cat := range categories {
			for _, c := range cat.Candidates {
				fmt.Printf("%s\t%s\n", c.Name, c.Path)
			}
		}
		return nil
	}

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
