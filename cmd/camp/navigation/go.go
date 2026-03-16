package navigation

import (
	"context"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/pins"
	"github.com/Obedience-Corp/camp/internal/state"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	Cmd.RunE = runGo

	// Custom help to show dynamic shortcuts from campaign config
	defaultHelp := Cmd.HelpFunc()
	Cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
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

	Cmd.Flags().Bool("print", false, "Print path only (for shell integration)")
	Cmd.Flags().StringArrayP("command", "c", nil, "Run command from directory (can be repeated for args)")
	Cmd.Flags().Bool("root", false, "Jump to campaign root (ignore last location)")
	Cmd.Flags().BoolP("list", "l", false, "List available sub-shortcuts for a project")
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
	configMappings := nav.BuildCategoryMappings(cfg.Shortcuts())

	// Handle toggle keyword: "t" or "toggle"
	if len(args) > 0 && (args[0] == "toggle" || args[0] == "t") {
		return handleToggle(ctx, campaignRoot, printOnly)
	}

	// Check if the first arg is a custom navigation shortcut with non-standard path
	if len(args) > 0 {
		shortcutName := args[0]
		if sc, ok := cfg.Shortcuts()[shortcutName]; ok && sc.IsNavigation() {
			// If this is a custom path (not a standard directory), use direct navigation
			// Standard paths are handled below via ParseShortcut for fuzzy search support
			if !nav.IsStandardPath(sc.Path) {
				return handleCustomNavShortcut(ctx, sc, campaignRoot, printOnly, command)
			}
		}
	}

	// Parse shortcuts using config mappings resolved from the campaign config.
	result := nav.ParseShortcut(args, configMappings)

	// Check for sub-shortcut in remaining args
	// Example: "camp p fest cli" -> result.Query="fest", subShortcut="cli"
	var subShortcut string
	queryParts := strings.Fields(result.Query)
	if len(queryParts) > 1 {
		result.Query = queryParts[0]
		subShortcut = queryParts[1]
	}

	// When no shortcut matched, check if the query matches a pin name
	if !result.IsShortcut && result.Query != "" {
		if pinPath, ok := resolvePin(campaignRoot, result.Query); ok {
			if len(command) > 0 {
				execResult, err := nav.ExecInDir(ctx, pinPath, command)
				if err != nil {
					return err
				}
				if execResult.ExitCode != 0 {
					return camperrors.NewCommand("", execResult.ExitCode, "", nil)
				}
				return nil
			}
			// Save current location (source) so toggle can return here
			cwd, _ := os.Getwd()
			_ = state.SetLastLocation(ctx, campaignRoot, cwd)
			if printOnly {
				fmt.Println(pinPath)
			} else {
				fmt.Printf("cd %s\n", pinPath)
			}
			return nil
		}
	}

	// Command execution mode
	if len(command) > 0 {
		execResult, err := nav.ExecInCategory(ctx, result.Category, command)
		if err != nil {
			return err
		}
		if execResult.ExitCode != 0 {
			return camperrors.NewCommand("", execResult.ExitCode, "", nil)
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
				return camperrors.Wrap(err, "failed to get current directory")
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

		// Save current location (source) so toggle can return here
		if result.Category != nav.CategoryAll || !forceRoot {
			cwd, _ := os.Getwd()
			_ = state.SetLastLocation(ctx, rootResult.Path, cwd)
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

	// Handle nested path queries containing "/"
	// e.g. "cgo de festival_app/src" -> resolve "festival_app" then append "src"
	if strings.Contains(result.Query, "/") && result.IsShortcut {
		parts := strings.SplitN(result.Query, "/", 2)
		targetName := parts[0]
		subPath := parts[1]

		// Resolve the first segment via the index
		resolveResult, err := index.Resolve(ctx, index.ResolveOptions{
			CampaignRoot: jumpResult.Path,
			Category:     result.Category,
			Query:        targetName,
		})
		if err == nil {
			// Append the subpath and verify it exists
			nestedPath := filepath.Join(resolveResult.Path, subPath)
			if info, statErr := os.Stat(nestedPath); statErr == nil && info.IsDir() {
				cwd, _ := os.Getwd()
				_ = state.SetLastLocation(ctx, jumpResult.Path, cwd)
				if printOnly {
					fmt.Println(nestedPath)
				} else {
					fmt.Printf("cd %s\n", nestedPath)
				}
				return nil
			}
			// Path doesn't exist — fall through to standard resolution
		}
		// Index resolution failed — fall through to standard resolution
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
			return cmdutil.FormatSubShortcutError(subErr)
		}
		return err
	}

	// Handle --list flag: show available sub-shortcuts for the matched project
	if listShortcuts {
		return listProjectShortcuts(resolveResult)
	}

	// Save current location (source) so toggle can return here
	cwd, _ := os.Getwd()
	_ = state.SetLastLocation(ctx, jumpResult.Path, cwd)

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

// handleToggle jumps to the last visited location from navigation history.
// It saves the current directory before jumping so repeated calls alternate
// between two locations, similar to "cd -".
func handleToggle(ctx context.Context, campaignRoot string, printOnly bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return camperrors.Wrap(err, "failed to get current directory")
	}

	lastLoc, err := state.GetLastLocation(ctx, campaignRoot)
	if err != nil || lastLoc == "" {
		return fmt.Errorf("no previous location in history")
	}

	cwdReal, _ := evalSymlinks(cwd)
	lastReal, _ := evalSymlinks(lastLoc)
	if cwdReal == lastReal {
		return fmt.Errorf("already at last visited location")
	}

	// Save current location so calling toggle again bounces back
	_ = state.SetLastLocation(ctx, campaignRoot, cwd)

	if printOnly {
		fmt.Println(lastLoc)
	} else {
		fmt.Printf("cd %s\n", lastLoc)
	}
	return nil
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
		if execResult.ExitCode != 0 {
			return camperrors.NewCommand("", execResult.ExitCode, "", nil)
		}
		return nil
	}

	// Save current location (source) so toggle can return here
	cwd, _ := os.Getwd()
	_ = state.SetLastLocation(ctx, campaignRoot, cwd)

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
	if len(cfg.Shortcuts()) > 0 {
		return formatConfigShortcuts(cfg.Shortcuts())
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
	return "Shortcuts: None configured. Add shortcuts to .campaign/settings/jumps.yaml\n"
}

// formatConfigShortcuts formats configured shortcuts for help output.
func formatConfigShortcuts(shortcuts map[string]config.ShortcutConfig) string {
	var sb strings.Builder
	sb.WriteString("Available shortcuts (from .campaign/settings/jumps.yaml):\n")

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

// resolvePin checks if the query matches a pin name and returns its absolute path.
func resolvePin(campaignRoot, query string) (string, bool) {
	storePath := config.PinsConfigPath(campaignRoot)
	pins.MigrateLegacyStore(
		campaignRoot,
		filepath.Join(campaignRoot, config.CampaignDir, "pins.json"),
		storePath,
	)
	store := pins.NewStore(storePath)
	if err := store.Load(); err != nil {
		return "", false
	}
	pin, ok := store.Get(query)
	if !ok {
		return "", false
	}
	// After migration all paths should be relative; reject any remaining absolute paths
	if filepath.IsAbs(pin.Path) {
		return "", false
	}
	return filepath.Join(campaignRoot, pin.Path), true
}
