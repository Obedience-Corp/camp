package navigation

import (
	"errors"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/shortcuts"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var ShortcutsCmd = &cobra.Command{
	Use:   "shortcuts",
	Short: "List all available shortcuts",
	Long: `List all navigation and command shortcuts from .campaign/campaign.yaml.

Navigation shortcuts (path-based):
  These shortcuts jump to directories within the campaign.
  Usage: camp go <shortcut>

Command shortcuts (command-based):
  These shortcuts execute commands from specified directories.
  Usage: camp run <shortcut> [args...]

Default shortcuts are added when you run 'camp init'.
You can customize shortcuts by editing .campaign/campaign.yaml.`,
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

	// Flags for campaign-level shortcuts (metadata, not scope selectors)
	shortcutsAddCmd.Flags().StringP("description", "d", "", "Help text for the shortcut")
	shortcutsAddCmd.Flags().StringP("concept", "c", "", "Command group for expansion")
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
