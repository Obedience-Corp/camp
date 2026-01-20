package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/nav"
	"github.com/obediencecorp/camp/internal/nav/index"
	"github.com/obediencecorp/camp/internal/state"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var goCmd = &cobra.Command{
	Use:   "go [shortcut] [query...]",
	Short: "Navigate to campaign directories",
	Long: `Navigate within the campaign using shortcuts.

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
	goCmd.GroupID = "navigation"

	// Custom help to show dynamic shortcuts from campaign config
	defaultHelp := goCmd.HelpFunc()
	goCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		// Generate dynamic shortcuts section
		shortcutsSection := formatShortcutsHelp()

		// Temporarily prepend shortcuts to Long description
		originalLong := cmd.Long
		cmd.Long = shortcutsSection + "\n" + originalLong

		// Call default help
		defaultHelp(cmd, args)

		// Restore original
		cmd.Long = originalLong
	})

	goCmd.Flags().Bool("print", false, "Print path only (for shell integration)")
	goCmd.Flags().StringArrayP("command", "c", nil, "Run command from directory (can be repeated for args)")
	goCmd.Flags().Bool("root", false, "Jump to campaign root (ignore last location)")
	goCmd.Flags().BoolP("list", "l", false, "List available sub-shortcuts for a project")
}

func runGo(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	printOnly, _ := cmd.Flags().GetBool("print")
	command, _ := cmd.Flags().GetStringArray("command")
	forceRoot, _ := cmd.Flags().GetBool("root")
	listShortcuts, _ := cmd.Flags().GetBool("list")

	// Load campaign config to get custom shortcuts
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return err
	}

	// Build category mappings from config shortcuts
	// This allows config shortcuts to work with fuzzy search
	configMappings := buildCategoryMappings(cfg.Shortcuts)

	// Check if the first arg is a custom navigation shortcut with non-standard path
	if len(args) > 0 {
		shortcutName := args[0]
		if sc, ok := cfg.Shortcuts[shortcutName]; ok && sc.IsNavigation() {
			// If this is a custom path (not a standard directory), use direct navigation
			// Standard paths are handled below via ParseShortcut for fuzzy search support
			if !isStandardPath(sc.Path) {
				return handleCustomNavShortcut(ctx, sc, campaignRoot, printOnly, command)
			}
		}
	}

	// Parse shortcuts using config mappings (with hardcoded defaults as fallback)
	result := nav.ParseShortcut(args, configMappings)

	// Check for sub-shortcut in remaining args
	// Example: "camp p fest cli" -> result.Query="fest", subShortcut="cli"
	var subShortcut string
	queryParts := strings.Fields(result.Query)
	if len(queryParts) > 1 {
		result.Query = queryParts[0]
		subShortcut = queryParts[1]
	}

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
		SubShortcut:  subShortcut,
	})
	if err != nil {
		// Handle invalid sub-shortcut error
		if subErr, ok := err.(*index.InvalidSubShortcutError); ok {
			return formatSubShortcutError(subErr)
		}
		return err
	}

	// Handle --list flag: show available sub-shortcuts for the matched project
	if listShortcuts {
		return listProjectShortcuts(resolveResult)
	}

	// Save this as the last location
	_ = state.SetLastLocation(ctx, jumpResult.Path, resolveResult.Path)

	// Multiple matches - inform user
	if resolveResult.HasMultipleMatches() && !printOnly {
		fmt.Fprintln(os.Stderr, ui.Warning("Multiple matches found:"))
		for _, m := range resolveResult.Matches {
			fmt.Fprintf(os.Stderr, "  %s %s\n", ui.BulletIcon(), ui.Dim(m.Name))
		}
		fmt.Fprintf(os.Stderr, "%s %s\n", ui.Label("Using best match:"), ui.Value(resolveResult.Name))
	}

	if printOnly {
		fmt.Println(resolveResult.Path)
	} else {
		fmt.Printf("cd %s\n", resolveResult.Path)
	}
	return nil
}

// formatSubShortcutError formats an InvalidSubShortcutError for user display.
func formatSubShortcutError(err *index.InvalidSubShortcutError) error {
	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("Error: Unknown shortcut '%s' for project '%s'\n",
		err.SubShortcut, err.ProjectName))

	if len(err.AvailableNames) > 0 {
		msg.WriteString("Available shortcuts: ")
		msg.WriteString(strings.Join(err.AvailableNames, ", "))
		msg.WriteString("\n")
	} else {
		msg.WriteString("No shortcuts configured for this project.\n")
	}

	msg.WriteString("\nSee: camp shortcuts --help")

	return fmt.Errorf(msg.String())
}

// listProjectShortcuts displays available sub-shortcuts for a project.
func listProjectShortcuts(result *index.ResolveResult) error {
	if result.Target == nil {
		fmt.Printf("%s: no target information available\n", result.Name)
		return nil
	}

	t := result.Target
	fmt.Printf("%s shortcuts:\n", ui.Accent(result.Name))

	if !t.HasShortcuts() {
		fmt.Printf("  %s\n", ui.Dim("(no shortcuts configured - jumps to project root)"))
		return nil
	}

	// Get sorted shortcut names
	names := t.ShortcutNames()
	for _, name := range names {
		path := t.Shortcuts[name]
		fmt.Printf("  %-12s %s\n", ui.Accent(name), ui.Dim(path))
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

// standardPaths maps standard directory paths to their nav categories.
var standardPaths = map[string]nav.Category{
	"projects/":     nav.CategoryProjects,
	"projects":      nav.CategoryProjects,
	"worktrees/":    nav.CategoryWorktrees,
	"worktrees":     nav.CategoryWorktrees,
	"festivals/":    nav.CategoryFestivals,
	"festivals":     nav.CategoryFestivals,
	"ai_docs/":      nav.CategoryAIDocs,
	"ai_docs":       nav.CategoryAIDocs,
	"docs/":         nav.CategoryDocs,
	"docs":          nav.CategoryDocs,
	"corpus/":       nav.CategoryCorpus,
	"corpus":        nav.CategoryCorpus,
	"code_reviews/": nav.CategoryCodeReviews,
	"code_reviews":  nav.CategoryCodeReviews,
	"pipelines/":    nav.CategoryPipelines,
	"pipelines":     nav.CategoryPipelines,
}

// isStandardPath returns true if the path maps to a known category.
func isStandardPath(path string) bool {
	_, ok := standardPaths[path]
	return ok
}

// buildCategoryMappings converts config shortcuts to nav.Category mappings.
// Only shortcuts with standard paths are included; custom paths are handled separately.
func buildCategoryMappings(shortcuts map[string]config.ShortcutConfig) map[string]nav.Category {
	mappings := make(map[string]nav.Category)
	for name, sc := range shortcuts {
		if sc.IsNavigation() {
			if cat, ok := standardPaths[sc.Path]; ok {
				mappings[name] = cat
			}
		}
	}
	return mappings
}

// formatShortcutsHelp generates the shortcuts section for help output.
// Only shows shortcuts from campaign.yaml - no hardcoded defaults.
func formatShortcutsHelp() string {
	ctx := context.Background()

	// Try to load campaign config
	cfg, _, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		// Not in campaign
		return formatNotInCampaignMessage()
	}

	// In campaign - show configured shortcuts only
	if len(cfg.Shortcuts) > 0 {
		return formatConfigShortcuts(cfg.Shortcuts)
	}

	// Campaign exists but no shortcuts configured
	return formatNoShortcutsMessage()
}

// formatNotInCampaignMessage returns message when not in a campaign.
func formatNotInCampaignMessage() string {
	return "Shortcuts: Not in a campaign. Run 'camp init' to create one.\n"
}

// formatNoShortcutsMessage returns message when campaign has no shortcuts.
func formatNoShortcutsMessage() string {
	return "Shortcuts: None configured. Add shortcuts to .campaign/campaign.yaml\n"
}

// formatConfigShortcuts formats configured shortcuts for help output.
func formatConfigShortcuts(shortcuts map[string]config.ShortcutConfig) string {
	var sb strings.Builder
	sb.WriteString("Available shortcuts (from .campaign/campaign.yaml):\n")

	// Separate navigation shortcuts only
	navShortcuts := make(map[string]config.ShortcutConfig)
	for key, sc := range shortcuts {
		if sc.IsNavigation() {
			navShortcuts[key] = sc
		}
	}

	if len(navShortcuts) == 0 {
		sb.WriteString("  (no navigation shortcuts configured)\n")
		return sb.String()
	}

	// Sort and display
	keys := make([]string, 0, len(navShortcuts))
	for k := range navShortcuts {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		sc := navShortcuts[key]
		path := strings.TrimSuffix(sc.Path, "/")
		sb.WriteString(fmt.Sprintf("  %-4s = %s\n", key, path))
	}

	return sb.String()
}
