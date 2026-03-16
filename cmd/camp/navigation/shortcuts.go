package navigation

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/shortcuts"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var ShortcutsCmd = &cobra.Command{
	Use:   "shortcuts",
	Short: "List all available shortcuts",
	Long: `List all navigation and command shortcuts from .campaign/settings/jumps.yaml.

Navigation shortcuts (path-based):
  These shortcuts jump to directories within the campaign.
  Usage: camp go <shortcut>

Command shortcuts (command-based):
  These shortcuts execute commands from specified directories.
  Usage: camp run <shortcut> [args...]

Default shortcuts are added when you run 'camp init'.
You can customize shortcuts by editing .campaign/settings/jumps.yaml.`,
	Example: `  camp shortcuts              # List all shortcuts
  camp go api                 # Use navigation shortcut
  camp run build              # Use command shortcut`,
	Aliases: []string{"sc"},
	GroupID: "navigation",
	RunE:    runShortcuts,
}

var shortcutsAddCmd = &cobra.Command{
	Use:   "add [name] [path] or [project] [name] [path]",
	Short: "Add a shortcut (campaign-level or project sub-shortcut)",
	Long: `Add a shortcut for quick navigation.

Campaign-level shortcut (2 args):
  Adds a navigation shortcut to .campaign/settings/jumps.yaml.
  Usage: camp shortcuts add <name> <path>

Project sub-shortcut (3 args):
  Adds a sub-directory shortcut within a project.
  Usage: camp shortcuts add <project> <name> <path>

With no arguments, launches an interactive TUI for entering
shortcut details.`,
	Example: `  camp shortcuts add                                  Interactive TUI mode
  camp shortcuts add api projects/api-service/        Campaign shortcut
  camp shortcuts add api projects/api/ -d "API svc"   With description
  camp shortcuts add cfg "" -c config                 Concept-only shortcut
  camp shortcuts add camp default cmd/camp/            Project sub-shortcut`,
	Args: cobra.MaximumNArgs(3),
	RunE: runShortcutsAdd,
}

var shortcutsRemoveCmd = &cobra.Command{
	Use:   "remove <name> or <project> <name>",
	Short: "Remove a shortcut (campaign-level or project sub-shortcut)",
	Long: `Remove a shortcut.

Campaign-level shortcut (1 arg):
  Usage: camp shortcuts remove <name>

Project sub-shortcut (2 args):
  Usage: camp shortcuts remove <project> <name>`,
	Example: `  camp shortcuts remove api                           Remove campaign shortcut
  camp shortcuts remove festival-methodology cli      Remove project sub-shortcut`,
	Aliases: []string{"rm"},
	Args:    cobra.RangeArgs(1, 2),
	RunE:    runShortcutsRemove,
}

var shortcutsDiffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show differences between current and default shortcuts",
	Long: `Compare your campaign's shortcuts against the current defaults.

Shows:
  + Missing    defaults not in your config (available to add)
  - Stale      auto-generated shortcuts no longer in defaults
  ~ Modified   shortcuts where path or concept differs from default
  = Up to date shortcuts matching defaults (count only)
  * Custom     user-defined shortcuts (always preserved)

Run 'camp shortcuts reset' to apply missing defaults and remove stale entries.`,
	Example: `  camp shortcuts diff`,
	RunE:    runShortcutsDiff,
}

var shortcutsResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset auto-generated shortcuts to current defaults",
	Long: `Reset shortcuts to match current defaults while preserving user-defined shortcuts.

Default behavior:
  - Adds missing default shortcuts
  - Removes stale auto-generated shortcuts (no longer in defaults)
  - Updates modified auto-generated shortcuts to match defaults
  - Preserves all user-defined shortcuts

With --all:
  - Replaces entire shortcuts config with defaults
  - Removes all user-defined shortcuts (with confirmation)

With --dry-run:
  - Shows what would change without saving`,
	Example: `  camp shortcuts reset             # Reset auto shortcuts, preserve custom
  camp shortcuts reset --dry-run   # Preview changes
  camp shortcuts reset --all       # Full reset (drops custom shortcuts)`,
	RunE: runShortcutsReset,
}

var shortcutsListCmd = &cobra.Command{
	Use:   "list [project]",
	Short: "List shortcuts for a specific project",
	Long: `List all sub-shortcuts configured for a specific project.

If no project is specified, lists all campaign shortcuts.`,
	Example: `  camp shortcuts list festival-methodology
  camp shortcuts list fest  # Fuzzy match`,
	Args: cobra.MaximumNArgs(1),
	RunE: runShortcutsList,
}

func init() {
	// Add subcommands
	ShortcutsCmd.AddCommand(shortcutsAddCmd)
	ShortcutsCmd.AddCommand(shortcutsRemoveCmd)
	ShortcutsCmd.AddCommand(shortcutsListCmd)
	ShortcutsCmd.AddCommand(shortcutsDiffCmd)
	ShortcutsCmd.AddCommand(shortcutsResetCmd)

	// Flags for campaign-level shortcuts (metadata, not scope selectors)
	shortcutsAddCmd.Flags().StringP("description", "d", "", "Help text for the shortcut")
	shortcutsAddCmd.Flags().StringP("concept", "c", "", "Command group for expansion")

	// Flags for reset
	shortcutsResetCmd.Flags().Bool("all", false, "Reset all shortcuts including user-defined ones")
	shortcutsResetCmd.Flags().Bool("dry-run", false, "Show what would change without saving")
}

func runShortcuts(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Load campaign config to get custom shortcuts
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		// Not in a campaign - just show defaults
		return printDefaultShortcuts()
	}

	return printAllShortcuts(cfg, campaignRoot)
}

func printDefaultShortcuts() error {
	fmt.Println(ui.Warning("Not in a campaign"))
	fmt.Println()
	fmt.Printf("Run %s to create a new campaign with default shortcuts.\n",
		ui.Accent("camp init"))

	return nil
}

func printAllShortcuts(cfg *config.CampaignConfig, _ string) error {
	fmt.Println(ui.Subheader("Shortcuts"))
	fmt.Printf("Campaign: %s\n", ui.Accent(cfg.Name))
	fmt.Println()

	if len(cfg.Shortcuts()) == 0 {
		fmt.Println(ui.Dim("No shortcuts configured."))
		fmt.Println()
		fmt.Printf("To add shortcuts, edit %s:\n", ui.Accent(".campaign/settings/jumps.yaml"))
		fmt.Println()
		fmt.Println(ui.Dim("  shortcuts:"))
		fmt.Println(ui.Dim("    api:"))
		fmt.Println(ui.Dim("      path: \"projects/api-service\""))
		fmt.Println(ui.Dim("      description: \"Jump to API\""))
		fmt.Println(ui.Dim("    build:"))
		fmt.Println(ui.Dim("      command: \"just build\""))
		fmt.Println(ui.Dim("      description: \"Build all\""))
		return nil
	}

	// Separate navigation and command shortcuts
	navShortcuts := make(map[string]config.ShortcutConfig)
	cmdShortcuts := make(map[string]config.ShortcutConfig)

	for key, sc := range cfg.Shortcuts() {
		if sc.IsNavigation() {
			navShortcuts[key] = sc
		}
		if sc.IsCommand() {
			cmdShortcuts[key] = sc
		}
	}

	// Display navigation shortcuts
	if len(navShortcuts) > 0 {
		fmt.Printf("%s\n", ui.Info("Navigation (use with: camp go <shortcut>)"))
		keys := make([]string, 0, len(navShortcuts))
		for k := range navShortcuts {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			sc := navShortcuts[key]
			desc := ""
			if sc.Description != "" {
				desc = ui.Dim(" # " + sc.Description)
			}
			// Show concept if available (indicates command expansion support)
			conceptCol := ui.Dim("(nav only)")
			if sc.Concept != "" {
				conceptCol = ui.Success(fmt.Sprintf("→ %s", sc.Concept))
			}
			fmt.Printf("  %s %s %-14s %s%s\n",
				ui.Accent(fmt.Sprintf("%-10s", key)),
				ui.ArrowIcon(),
				ui.Value(sc.Path),
				conceptCol,
				desc)
		}
		fmt.Println()
	}

	// Display command shortcuts
	if len(cmdShortcuts) > 0 {
		fmt.Printf("%s\n", ui.Info("Commands (use with: camp run <shortcut>)"))
		keys := make([]string, 0, len(cmdShortcuts))
		for k := range cmdShortcuts {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			sc := cmdShortcuts[key]
			desc := ""
			if sc.Description != "" {
				desc = ui.Dim(" # " + sc.Description)
			}

			// Show command with workdir if specified
			cmdDisplay := sc.Command
			if sc.WorkDir != "" {
				cmdDisplay = fmt.Sprintf("[%s] %s", sc.WorkDir, sc.Command)
			}

			// Truncate long commands
			if len(cmdDisplay) > 50 {
				cmdDisplay = cmdDisplay[:47] + "..."
			}

			fmt.Printf("  %s %s %s%s\n",
				ui.Accent(fmt.Sprintf("%-10s", key)),
				ui.ArrowIcon(),
				ui.Value(cmdDisplay),
				desc)
		}
	}

	return nil
}

// runShortcutsAdd adds a campaign-level shortcut (2 args) or a project sub-shortcut (3 args).
func runShortcutsAdd(cmd *cobra.Command, args []string) error {
	// Campaign-level shortcut: 2 args
	if len(args) == 2 {
		return runShortcutsAddJump(cmd, args)
	}

	ctx := cmd.Context()

	// Load campaign config
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return err
	}

	var projectName, shortcutName, shortcutPath string

	// TUI mode if no args provided
	if len(args) == 0 {
		result, err := shortcuts.RunAddSubShortcutTUI(ctx, root)
		if err != nil {
			if errors.Is(err, shortcuts.ErrAborted) {
				return fmt.Errorf("shortcut creation cancelled")
			}
			return err
		}
		projectName = result.ProjectName
		shortcutName = result.ShortcutName
		shortcutPath = result.ShortcutPath
	} else if len(args) == 3 {
		projectName = args[0]
		shortcutName = args[1]
		shortcutPath = args[2]
	} else {
		return fmt.Errorf("expected 0, 2, or 3 arguments, got %d\n  2 args: camp shortcuts add <name> <path> (campaign shortcut)\n  3 args: camp shortcuts add <project> <name> <path> (project sub-shortcut)", len(args))
	}

	// Find the project (fuzzy match)
	projectIdx := findProjectIndex(cfg.Projects, projectName)
	if projectIdx == -1 {
		return fmt.Errorf("project %q not found (run 'camp projects' to see available projects)", projectName)
	}

	project := &cfg.Projects[projectIdx]

	// Validate path exists
	fullPath := filepath.Join(root, project.Path, shortcutPath)
	if stat, err := os.Stat(fullPath); err != nil || !stat.IsDir() {
		return fmt.Errorf("path does not exist or is not a directory: %s", fullPath)
	}

	// Initialize shortcuts map if nil
	if project.Shortcuts == nil {
		project.Shortcuts = make(map[string]string)
	}

	// Check if shortcut already exists
	if existing, ok := project.Shortcuts[shortcutName]; ok {
		fmt.Printf("%s Updating shortcut '%s' for project '%s'\n",
			ui.WarningIcon(), shortcutName, project.Name)
		fmt.Printf("  Old: %s\n", ui.Dim(existing))
		fmt.Printf("  New: %s\n", ui.Value(shortcutPath))
	} else {
		fmt.Printf("%s Adding shortcut '%s' to project '%s'\n",
			ui.SuccessIcon(), shortcutName, project.Name)
	}

	// Add/update the shortcut
	project.Shortcuts[shortcutName] = shortcutPath

	// Save config
	if err := config.SaveCampaignConfig(ctx, root, cfg); err != nil {
		return camperrors.Wrap(err, "failed to save config")
	}

	fmt.Printf("\n%s %s %s %s\n",
		ui.Label("Usage:"),
		ui.Accent("camp p"),
		ui.Value(projectName),
		ui.Value(shortcutName))

	return nil
}

// runShortcutsRemove removes a campaign-level shortcut (1 arg) or a project sub-shortcut (2 args).
func runShortcutsRemove(cmd *cobra.Command, args []string) error {
	// Campaign-level shortcut: 1 arg
	if len(args) == 1 {
		return runShortcutsRemoveJump(cmd, args)
	}

	ctx := cmd.Context()
	projectName := args[0]
	shortcutName := args[1]

	// Load campaign config
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return err
	}

	// Find the project (fuzzy match)
	projectIdx := findProjectIndex(cfg.Projects, projectName)
	if projectIdx == -1 {
		return fmt.Errorf("project %q not found", projectName)
	}

	project := &cfg.Projects[projectIdx]

	// Check if shortcut exists
	if project.Shortcuts == nil {
		return fmt.Errorf("project '%s' has no shortcuts configured", project.Name)
	}

	if _, ok := project.Shortcuts[shortcutName]; !ok {
		return fmt.Errorf("shortcut '%s' not found in project '%s'", shortcutName, project.Name)
	}

	// Remove the shortcut
	delete(project.Shortcuts, shortcutName)

	// Save config
	if err := config.SaveCampaignConfig(ctx, root, cfg); err != nil {
		return camperrors.Wrap(err, "failed to save config")
	}

	fmt.Printf("%s Removed shortcut '%s' from project '%s'\n",
		ui.SuccessIcon(), shortcutName, project.Name)

	return nil
}

// runShortcutsList lists shortcuts for a specific project or all campaign shortcuts.
func runShortcutsList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Load campaign config
	cfg, _, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return err
	}

	// If no project specified, show all shortcuts
	if len(args) == 0 {
		return printAllShortcuts(cfg, "")
	}

	projectName := args[0]

	// Find the project (fuzzy match)
	projectIdx := findProjectIndex(cfg.Projects, projectName)
	if projectIdx == -1 {
		return fmt.Errorf("project %q not found", projectName)
	}

	project := cfg.Projects[projectIdx]

	// Display project shortcuts
	fmt.Printf("%s shortcuts:\n", ui.Accent(project.Name))

	if len(project.Shortcuts) == 0 {
		fmt.Printf("  %s\n", ui.Dim("(no shortcuts configured - jumps to project root)"))
		return nil
	}

	// Get sorted shortcut names
	names := make([]string, 0, len(project.Shortcuts))
	for name := range project.Shortcuts {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		path := project.Shortcuts[name]
		fmt.Printf("  %-12s %s\n", ui.Accent(name), ui.Dim(path))
	}

	return nil
}

// findProjectIndex finds a project by name (exact or prefix match).
// Returns -1 if not found.
func findProjectIndex(projects []config.ProjectConfig, name string) int {
	// Try exact match first
	for i, p := range projects {
		if p.Name == name {
			return i
		}
	}

	// Try prefix match
	name = strings.ToLower(name)
	for i, p := range projects {
		if strings.HasPrefix(strings.ToLower(p.Name), name) {
			return i
		}
	}

	return -1
}

// runShortcutsAddJump adds a campaign-level shortcut to jumps.yaml.
func runShortcutsAddJump(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Load campaign config to get root
	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return err
	}

	var shortcutName, shortcutPath, description, concept string

	// TUI mode if no args provided
	if len(args) == 0 {
		result, err := shortcuts.RunAddJumpTUI(ctx, root)
		if err != nil {
			if errors.Is(err, shortcuts.ErrAborted) {
				return fmt.Errorf("shortcut creation cancelled")
			}
			return err
		}
		shortcutName = result.Name
		shortcutPath = result.Path
		description = result.Description
		concept = result.Concept
	} else {
		shortcutName = args[0]
		// Get path (optional, empty string if not provided)
		if len(args) > 1 {
			shortcutPath = args[1]
		}
		// Get flags
		description, _ = cmd.Flags().GetString("description")
		concept, _ = cmd.Flags().GetString("concept")

		// Validate: must have path or concept
		if shortcutPath == "" && concept == "" {
			return fmt.Errorf("shortcut must have a path or concept (use -c to specify concept)")
		}
	}

	// Load jumps config
	jumps, err := config.LoadJumpsConfig(ctx, root)
	if err != nil {
		return camperrors.Wrap(err, "failed to load jumps config")
	}

	// Create default jumps config if nil
	if jumps == nil {
		defaultJumps := config.DefaultJumpsConfig()
		jumps = &defaultJumps
	}

	// Initialize shortcuts map if nil
	if jumps.Shortcuts == nil {
		jumps.Shortcuts = make(map[string]config.ShortcutConfig)
	}

	// Validate path exists if provided
	if shortcutPath != "" {
		fullPath := filepath.Join(root, shortcutPath)
		if stat, err := os.Stat(fullPath); err != nil || !stat.IsDir() {
			return fmt.Errorf("path does not exist or is not a directory: %s", fullPath)
		}
	}

	// Check if shortcut already exists
	if existing, ok := jumps.Shortcuts[shortcutName]; ok {
		fmt.Printf("%s Updating shortcut '%s'\n", ui.WarningIcon(), shortcutName)
		if existing.Path != "" {
			fmt.Printf("  Old path: %s\n", ui.Dim(existing.Path))
		}
		if shortcutPath != "" {
			fmt.Printf("  New path: %s\n", ui.Value(shortcutPath))
		}
	} else {
		fmt.Printf("%s Adding shortcut '%s'\n", ui.SuccessIcon(), shortcutName)
	}

	// Create shortcut config
	sc := config.ShortcutConfig{
		Path:        shortcutPath,
		Description: description,
		Concept:     concept,
	}

	// Add/update the shortcut
	jumps.Shortcuts[shortcutName] = sc

	// Save jumps config
	if err := config.SaveJumpsConfig(ctx, root, jumps); err != nil {
		return camperrors.Wrap(err, "failed to save jumps config")
	}

	// Show usage info
	fmt.Println()
	if shortcutPath != "" {
		fmt.Printf("%s %s %s\n", ui.Label("Navigate:"), ui.Accent("camp go"), ui.Value(shortcutName))
	}
	if concept != "" {
		fmt.Printf("%s %s %s <command>\n", ui.Label("Expand:"), ui.Accent("camp"), ui.Value(shortcutName))
	}

	return nil
}

// isAutoShortcut returns true if the shortcut was auto-generated (not user-defined).
// Legacy entries (empty Source) are checked against known defaults.
func isAutoShortcut(sc config.ShortcutConfig, key string, defaults map[string]config.ShortcutConfig) bool {
	if sc.Source == config.ShortcutSourceUser {
		return false
	}
	if sc.Source == config.ShortcutSourceAuto {
		return true
	}
	// Legacy (empty Source): treat as auto if it matches a known default by path
	if def, ok := defaults[key]; ok {
		return sc.Path == def.Path && sc.Concept == def.Concept
	}
	return false
}

// shortcutDiff holds the categorized differences between current and default shortcuts.
type shortcutDiff struct {
	missing  []string // default keys not in current config
	stale    []string // auto keys in current config not in defaults
	modified []string // same key, different path/concept (auto only)
	custom   []string // user-defined shortcuts
	matched  int      // count of shortcuts matching defaults
}

// computeShortcutDiff compares current shortcuts against defaults.
func computeShortcutDiff(current, defaults map[string]config.ShortcutConfig) shortcutDiff {
	var diff shortcutDiff

	// Find missing and modified defaults
	for key, def := range defaults {
		cur, exists := current[key]
		if !exists {
			diff.missing = append(diff.missing, key)
			continue
		}
		if cur.Path == def.Path && cur.Concept == def.Concept {
			diff.matched++
		} else if isAutoShortcut(cur, key, defaults) {
			diff.modified = append(diff.modified, key)
		} else {
			// User modified a default key — treat as custom
			diff.custom = append(diff.custom, key)
		}
	}

	// Find stale and custom shortcuts
	for key, sc := range current {
		if _, isDefault := defaults[key]; isDefault {
			continue // already handled above
		}
		if isAutoShortcut(sc, key, defaults) {
			diff.stale = append(diff.stale, key)
		} else {
			diff.custom = append(diff.custom, key)
		}
	}

	sort.Strings(diff.missing)
	sort.Strings(diff.stale)
	sort.Strings(diff.modified)
	sort.Strings(diff.custom)

	return diff
}

func runShortcutsDiff(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	cfg, _, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	current := cfg.Shortcuts()
	defaults := config.DefaultNavigationShortcuts()
	diff := computeShortcutDiff(current, defaults)

	hasDiff := len(diff.missing) > 0 || len(diff.stale) > 0 || len(diff.modified) > 0

	fmt.Println(ui.Subheader("Shortcuts Diff"))
	fmt.Printf("Campaign: %s\n", ui.Accent(cfg.Name))
	fmt.Println()

	if len(diff.missing) > 0 {
		fmt.Printf("  %s\n", ui.Info("Missing from your config (run 'camp shortcuts reset' to add):"))
		for _, key := range diff.missing {
			def := defaults[key]
			fmt.Printf("    %s %-10s %-20s %s\n",
				ui.Success("+"), ui.Accent(key), ui.Value(def.Path), ui.Dim(def.Description))
		}
		fmt.Println()
	}

	if len(diff.stale) > 0 {
		fmt.Printf("  %s\n", ui.Warning("Stale defaults (no longer in default set):"))
		for _, key := range diff.stale {
			sc := current[key]
			fmt.Printf("    %s %-10s %-20s %s\n",
				ui.Error("-"), ui.Accent(key), ui.Dim(sc.Path), ui.Dim("was auto-generated"))
		}
		fmt.Println()
	}

	if len(diff.modified) > 0 {
		fmt.Printf("  %s\n", ui.Warning("Modified (auto-generated, differs from default):"))
		for _, key := range diff.modified {
			cur := current[key]
			def := defaults[key]
			fmt.Printf("    %s %-10s yours: %-16s default: %s\n",
				ui.Warning("~"), ui.Accent(key), ui.Dim(cur.Path), ui.Value(def.Path))
		}
		fmt.Println()
	}

	if len(diff.custom) > 0 {
		fmt.Printf("  %s\n", ui.Info("Custom shortcuts (always preserved):"))
		for _, key := range diff.custom {
			sc := current[key]
			path := sc.Path
			if path == "" {
				path = sc.Command
			}
			fmt.Printf("    %s %-10s %s\n",
				ui.Dim("*"), ui.Accent(key), ui.Dim(path))
		}
		fmt.Println()
	}

	if diff.matched > 0 {
		fmt.Printf("  %s %d shortcuts match defaults\n", ui.SuccessIcon(), diff.matched)
	}

	if !hasDiff {
		fmt.Printf("\n  %s Shortcuts are up to date\n", ui.SuccessIcon())
	}

	return nil
}

func runShortcutsReset(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	resetAll, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	jumps, err := config.LoadJumpsConfig(ctx, root)
	if err != nil {
		return camperrors.Wrap(err, "failed to load jumps config")
	}
	if jumps == nil {
		defaultJumps := config.DefaultJumpsConfig()
		jumps = &defaultJumps
	}
	if jumps.Shortcuts == nil {
		jumps.Shortcuts = make(map[string]config.ShortcutConfig)
	}

	defaults := config.DefaultNavigationShortcuts()
	diff := computeShortcutDiff(jumps.Shortcuts, defaults)

	hasDiff := len(diff.missing) > 0 || len(diff.stale) > 0 || len(diff.modified) > 0

	if resetAll {
		// Count custom shortcuts that will be removed
		if len(diff.custom) > 0 && !dryRun {
			fmt.Printf("%s This will remove %d custom shortcut(s): %s\n",
				ui.WarningIcon(), len(diff.custom), strings.Join(diff.custom, ", "))
			fmt.Print("Continue? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				fmt.Println("Cancelled.")
				return nil
			}
		}

		if !hasDiff && len(diff.custom) == 0 {
			fmt.Printf("%s Shortcuts are already at defaults\n", ui.SuccessIcon())
			return nil
		}

		jumps.Shortcuts = defaults

		label := "Reset"
		if dryRun {
			label = "Would reset"
		}
		fmt.Println(ui.Subheader(fmt.Sprintf("Shortcuts %s (--all)", label)))
		fmt.Printf("  Replaced all shortcuts with %d defaults\n", len(defaults))
		if len(diff.custom) > 0 {
			fmt.Printf("  Removed %d custom shortcut(s)\n", len(diff.custom))
		}
	} else {
		if !hasDiff {
			fmt.Printf("%s Shortcuts are already up to date (auto shortcuts match defaults)\n", ui.SuccessIcon())
			if len(diff.custom) > 0 {
				fmt.Printf("  %d custom shortcut(s) preserved\n", len(diff.custom))
			}
			return nil
		}

		label := "Reset"
		if dryRun {
			label = "Would reset"
		}
		fmt.Println(ui.Subheader(fmt.Sprintf("Shortcuts %s", label)))
		fmt.Println()

		// Add missing defaults
		for _, key := range diff.missing {
			if !dryRun {
				jumps.Shortcuts[key] = defaults[key]
			}
			fmt.Printf("  %s  Added    %-10s %s %s\n",
				ui.SuccessIcon(), ui.Accent(key), ui.ArrowIcon(), ui.Value(defaults[key].Path))
		}

		// Remove stale auto shortcuts
		for _, key := range diff.stale {
			if !dryRun {
				delete(jumps.Shortcuts, key)
			}
			fmt.Printf("  %s  Removed  %-10s %s\n",
				ui.ErrorIcon(), ui.Accent(key), ui.Dim("(stale default)"))
		}

		// Update modified auto shortcuts
		for _, key := range diff.modified {
			if !dryRun {
				jumps.Shortcuts[key] = defaults[key]
			}
			fmt.Printf("  %s  Updated  %-10s %s %s\n",
				ui.WarningIcon(), ui.Accent(key), ui.ArrowIcon(), ui.Value(defaults[key].Path))
		}

		// Show preserved custom shortcuts
		for _, key := range diff.custom {
			fmt.Printf("  %s  Kept     %-10s %s\n",
				ui.BulletIcon(), ui.Accent(key), ui.Dim("(user-defined)"))
		}
	}

	if dryRun {
		fmt.Printf("\n  %s\n", ui.Dim("Dry run — no changes saved. Remove --dry-run to apply."))
		return nil
	}

	if err := config.SaveJumpsConfig(ctx, root, jumps); err != nil {
		return camperrors.Wrap(err, "failed to save jumps config")
	}

	fmt.Printf("\n  %s Saved to %s\n", ui.SuccessIcon(), ui.Dim(".campaign/settings/jumps.yaml"))
	return nil
}

// runShortcutsRemoveJump removes a campaign-level shortcut from jumps.yaml.
func runShortcutsRemoveJump(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	shortcutName := args[0]

	// Load campaign config to get root
	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return err
	}

	// Load jumps config
	jumps, err := config.LoadJumpsConfig(ctx, root)
	if err != nil {
		return camperrors.Wrap(err, "failed to load jumps config")
	}

	// Check if jumps config exists
	if jumps == nil || jumps.Shortcuts == nil {
		return fmt.Errorf("no shortcuts configured")
	}

	// Check if shortcut exists
	if _, ok := jumps.Shortcuts[shortcutName]; !ok {
		return fmt.Errorf("shortcut '%s' not found", shortcutName)
	}

	// Remove the shortcut
	delete(jumps.Shortcuts, shortcutName)

	// Save jumps config
	if err := config.SaveJumpsConfig(ctx, root, jumps); err != nil {
		return camperrors.Wrap(err, "failed to save jumps config")
	}

	fmt.Printf("%s Removed shortcut '%s'\n", ui.SuccessIcon(), shortcutName)

	return nil
}
