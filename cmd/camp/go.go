package main

import (
	"fmt"
	"os"

	"github.com/obediencecorp/camp/internal/nav"
	"github.com/obediencecorp/camp/internal/nav/index"
	"github.com/spf13/cobra"
)

var goCmd = &cobra.Command{
	Use:   "go [category] [query...]",
	Short: "Navigate to campaign directories",
	Long: `Navigate within the campaign using category shortcuts.

Category shortcuts:
  p  = projects       c  = corpus        f  = festivals
  a  = ai_docs        d  = docs          w  = worktrees
  r  = code_reviews   pi = pipelines

Usage patterns:
  camp go           Jump to campaign root
  camp go p         Jump to projects/
  camp go f         Jump to festivals/
  camp go p api     Fuzzy search projects/ for "api"

The --print flag outputs just the path for shell integration:
  cd "$(camp go p --print)"

The -c flag runs a command from the directory without changing to it:
  camp go p -c ls           List contents of projects/
  camp go f -c fest status  Run fest status from festivals/

Or use the cgo shell function for instant navigation:
  cgo p             Equivalent to: cd "$(camp go p --print)"
  cgo p -c ls       Run ls in projects/ without changing directory`,
	Example: `  camp go               # Jump to campaign root
  camp go p             # Jump to projects/
  camp go p api         # Fuzzy find "api" in projects/
  camp go p --print     # Print path (for shell scripts)
  camp go f -c ls       # List festivals/ without cd`,
	Aliases: []string{"g"},
	RunE:    runGo,
}

func init() {
	rootCmd.AddCommand(goCmd)

	goCmd.Flags().Bool("print", false, "Print path only (for shell integration)")
	goCmd.Flags().StringArrayP("command", "c", nil, "Run command from directory (can be repeated for args)")
}

func runGo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	printOnly, _ := cmd.Flags().GetBool("print")
	command, _ := cmd.Flags().GetStringArray("command")

	// Parse shortcuts
	result := nav.ParseShortcut(args, nil)

	// Command execution mode
	if len(command) > 0 {
		execResult, err := nav.ExecInCategory(ctx, result.Category, command)
		if err != nil {
			return err
		}
		// Exit with the command's exit code
		if execResult.ExitCode != 0 {
			os.Exit(execResult.ExitCode)
		}
		return nil
	}

	// Direct jump if no query
	if result.Query == "" {
		jumpResult, err := nav.DirectJump(ctx, result.Category)
		if err != nil {
			return err
		}

		if printOnly {
			fmt.Println(jumpResult.Path)
		} else {
			// Output cd command for user to copy/execute
			fmt.Printf("cd %s\n", jumpResult.Path)
		}
		return nil
	}

	// Has query - use resolve for fuzzy search
	// First get campaign root
	jumpResult, err := nav.DirectJump(ctx, nav.CategoryAll)
	if err != nil {
		return err
	}

	resolveResult, err := index.Resolve(ctx, index.ResolveOptions{
		CampaignRoot: jumpResult.Path,
		Category:     result.Category,
		Query:        result.Query,
	})
	if err != nil {
		return err
	}

	// Multiple matches - inform user
	if resolveResult.HasMultipleMatches() && !printOnly {
		fmt.Fprintf(os.Stderr, "Multiple matches found:\n")
		for _, m := range resolveResult.Matches {
			fmt.Fprintf(os.Stderr, "  %s\n", m.Name)
		}
		fmt.Fprintf(os.Stderr, "Using best match: %s\n", resolveResult.Name)
	}

	if printOnly {
		fmt.Println(resolveResult.Path)
	} else {
		fmt.Printf("cd %s\n", resolveResult.Path)
	}
	return nil
}
