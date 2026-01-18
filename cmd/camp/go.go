package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/nav"
	"github.com/obediencecorp/camp/internal/nav/index"
	"github.com/obediencecorp/camp/internal/state"
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
  camp go           Toggle between campaign root and last location
  camp go --root    Jump to campaign root (ignore toggle)
  camp go p         Jump to projects/
  camp go f         Jump to festivals/
  camp go p api     Fuzzy search projects/ for "api"

Toggle behavior (no args):
  - From anywhere: jump to campaign root, save current location
  - From campaign root: jump back to saved location

The --print flag outputs just the path for shell integration:
  cd "$(camp go p --print)"

The -c flag runs a command from the directory without changing to it:
  camp go p -c ls           List contents of projects/
  camp go f -c fest status  Run fest status from festivals/

Or use the cgo shell function for instant navigation:
  cgo               Toggle between root and last location
  cgo p             Equivalent to: cd "$(camp go p --print)"
  cgo p -c ls       Run ls in projects/ without changing directory`,
	Example: `  camp go               # Toggle: root ↔ last location
  camp go --root        # Force jump to campaign root
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
	goCmd.Flags().Bool("root", false, "Jump to campaign root (ignore last location)")
}

func runGo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	printOnly, _ := cmd.Flags().GetBool("print")
	command, _ := cmd.Flags().GetStringArray("command")
	forceRoot, _ := cmd.Flags().GetBool("root")

	// Load campaign config to get custom shortcuts
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return err
	}

	// Check if the first arg is a custom navigation shortcut
	if len(args) > 0 {
		shortcutName := args[0]
		if sc, ok := cfg.Shortcuts[shortcutName]; ok && sc.IsNavigation() {
			// This is a custom navigation shortcut
			return handleCustomNavShortcut(ctx, sc, campaignRoot, printOnly, command)
		}
	}

	// Parse built-in shortcuts
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
		// Get campaign root first for last location lookup
		rootResult, err := nav.DirectJump(ctx, nav.CategoryAll)
		if err != nil {
			return err
		}

		// If no category and no --root flag, implement toggle behavior
		if result.Category == nav.CategoryAll && !forceRoot {
			// Get current working directory to check if we're at root
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}

			// Normalize paths for comparison (resolve symlinks)
			cwdReal, _ := evalSymlinks(cwd)
			rootReal, _ := evalSymlinks(rootResult.Path)
			atRoot := cwdReal == rootReal

			var destPath string
			if atRoot {
				// At campaign root - toggle back to last location
				lastLoc, err := state.GetLastLocation(ctx, rootResult.Path)
				if err == nil && lastLoc != "" && lastLoc != rootResult.Path {
					destPath = lastLoc
				} else {
					// No last location - stay at root
					destPath = rootResult.Path
				}
			} else {
				// Not at root - save current location and jump to root
				_ = state.SetLastLocation(ctx, rootResult.Path, cwd)
				destPath = rootResult.Path
			}

			if printOnly {
				fmt.Println(destPath)
			} else {
				fmt.Printf("cd %s\n", destPath)
			}
			return nil
		}

		// Jump to the requested category (or root if no category specified)
		jumpResult, err := nav.DirectJump(ctx, result.Category)
		if err != nil {
			return err
		}

		// Save this as the last location (except when jumping to root with --root flag)
		if result.Category != nav.CategoryAll || !forceRoot {
			_ = state.SetLastLocation(ctx, rootResult.Path, jumpResult.Path)
		}

		if printOnly {
			fmt.Println(jumpResult.Path)
		} else {
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

	// Save this as the last location
	_ = state.SetLastLocation(ctx, jumpResult.Path, resolveResult.Path)

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

// handleCustomNavShortcut handles navigation to a custom path shortcut.
func handleCustomNavShortcut(ctx context.Context, sc config.ShortcutConfig, campaignRoot string, printOnly bool, command []string) error {
	// Jump to the custom path
	jumpResult, err := nav.JumpToPathFromRoot(ctx, campaignRoot, sc.Path)
	if err != nil {
		return err
	}

	// Command execution mode
	if len(command) > 0 {
		execResult, err := nav.ExecInDir(ctx, jumpResult.Path, command)
		if err != nil {
			return err
		}
		// Exit with the command's exit code
		if execResult.ExitCode != 0 {
			os.Exit(execResult.ExitCode)
		}
		return nil
	}

	// Save this as the last location
	_ = state.SetLastLocation(ctx, campaignRoot, jumpResult.Path)

	if printOnly {
		fmt.Println(jumpResult.Path)
	} else {
		fmt.Printf("cd %s\n", jumpResult.Path)
	}
	return nil
}

// evalSymlinks resolves symlinks in a path, returning the original path if resolution fails.
func evalSymlinks(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path, err
	}
	return resolved, nil
}
