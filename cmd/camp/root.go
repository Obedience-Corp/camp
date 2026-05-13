package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	attachpkg "github.com/Obedience-Corp/camp/cmd/camp/attach"
	cachepkg "github.com/Obedience-Corp/camp/cmd/camp/cache"
	dungeonpkg "github.com/Obedience-Corp/camp/cmd/camp/dungeon"
	initcmd "github.com/Obedience-Corp/camp/cmd/camp/init"
	intentpkg "github.com/Obedience-Corp/camp/cmd/camp/intent"
	leveragepkg "github.com/Obedience-Corp/camp/cmd/camp/leverage"
	navigationpkg "github.com/Obedience-Corp/camp/cmd/camp/navigation"
	projectpkg "github.com/Obedience-Corp/camp/cmd/camp/project"
	refspkg "github.com/Obedience-Corp/camp/cmd/camp/refs"
	registrypkg "github.com/Obedience-Corp/camp/cmd/camp/registry"
	skillspkg "github.com/Obedience-Corp/camp/cmd/camp/skills"
	worktreespkg "github.com/Obedience-Corp/camp/cmd/camp/worktrees"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/commands/release"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/version"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	cfgFile string
	noColor bool
	verbose bool
)

var rootCmd = &cobra.Command{
	Use:           "camp",
	Short:         "Campaign management CLI for multi-project AI workspaces",
	Version:       fmt.Sprintf("%s (built %s, commit %s)", version.Version, version.BuildDate, version.Commit),
	SilenceErrors: true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Skip color detection for completion commands to avoid
		// termenv interfering with zsh's completion state machine.
		if cmd.Name() == "complete" {
			ui.SetNoColor(true)
			return
		}
		ui.SetNoColor(noColor)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// When invoked without subcommand, show help
		return cmd.Help()
	},
}

// Execute runs the root command
func Execute() error {
	// Expand shortcuts before command execution
	expandShortcuts()

	// Rewrite `--flag value` → `--flag=value` for optional-value flags (those
	// with NoOptDefVal set on their pflag definition). Without this, cobra's
	// pflag library never consumes the space-separated next token, which
	// makes `camp intent add --campaign <name> "Title"` misparse <name> as a
	// positional arg.
	os.Args = normalizeOptionalValueFlagArgs(os.Args)

	// Try git-style plugin dispatch for unknown subcommands.
	// A camp-<name> binary on PATH becomes "camp <name> [args...]".
	if err := dispatchPlugin(); err != nil {
		if errors.Is(err, errPluginHandled) {
			return nil
		}
		return err
	}

	return rootCmd.Execute()
}

// expandShortcuts expands shortcut aliases in os.Args before cobra parses them.
// For example, "camp p list" becomes "camp project list" if "p" maps to "project".
func expandShortcuts() {
	firstArg, argIndex := findFirstPositionalArg(os.Args)
	if firstArg == "" {
		return
	}

	// Skip certain commands that shouldn't be expanded
	if firstArg == "help" || firstArg == "completion" {
		return
	}

	// Try to load shortcuts from campaign config
	shortcutMap := loadShortcutsForExpansion()
	if shortcutMap == nil {
		return
	}

	// Check if first arg is a shortcut with concept
	sc, ok := shortcutMap[firstArg]
	if !ok || sc.Concept == "" {
		return
	}

	// Expand the shortcut
	os.Args[argIndex] = sc.Concept
}

// loadShortcutsForExpansion loads shortcuts from campaign config if available.
// It merges campaign shortcuts with defaults, prioritizing concept mappings from defaults
// when the user's shortcuts don't specify a concept.
func loadShortcutsForExpansion() map[string]config.ShortcutConfig {
	ctx := context.Background()
	defaults := config.DefaultNavigationShortcuts()

	// Try to detect campaign root
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		// Not in a campaign - use default shortcuts only
		return defaults
	}

	// Load campaign config
	cfg, err := config.LoadCampaignConfig(ctx, campRoot)
	if err != nil {
		// Fall back to defaults
		return defaults
	}

	// Get campaign shortcuts
	campaignShortcuts := cfg.Shortcuts()

	// Merge: campaign shortcuts override defaults, but inherit Concept from defaults
	// if not specified in campaign config
	result := make(map[string]config.ShortcutConfig)

	// Start with defaults
	for k, v := range defaults {
		result[k] = v
	}

	// Merge campaign shortcuts, preserving Concept from defaults when not set
	for k, v := range campaignShortcuts {
		if defaultSc, hasDefault := defaults[k]; hasDefault && v.Concept == "" {
			// Inherit Concept from default if campaign doesn't specify one
			v.Concept = defaultSc.Concept
		}
		result[k] = v
	}

	return result
}

func init() {
	// Register template functions for styled help
	cobra.AddTemplateFunc("styleLabel", ui.Label)
	cobra.AddTemplateFunc("styleCategory", ui.Category)
	cobra.AddTemplateFunc("styleDim", ui.Dim)
	cobra.AddTemplateFunc("styleAccent", ui.Accent)
	cobra.AddTemplateFunc("styleBold", ui.BoldText)
	cobra.AddTemplateFunc("styleHeader", ui.Header)
	cobra.AddTemplateFunc("styleHelp", ui.StyleHelpText)
	cobra.AddTemplateFunc("cleanFlagUsages", cleanFlagUsages)

	// Set styled Long description for root command
	rootCmd.Long = styledLongDescription()

	// Apply styled help and usage templates
	rootCmd.SetHelpTemplate(styledHelpTemplate())
	rootCmd.SetUsageTemplate(styledUsageTemplate())

	// Define command groups for organized help output
	rootCmd.AddGroup(
		&cobra.Group{ID: "setup", Title: ui.Category("Setup Commands:")},
		&cobra.Group{ID: "campaign", Title: ui.Category("Campaign Commands:")},
		&cobra.Group{ID: "git", Title: ui.Category("Git Commands:")},
		&cobra.Group{ID: "navigation", Title: ui.Category("Navigation Commands:")},
		&cobra.Group{ID: "registry", Title: ui.Category("Registry Commands:")},
		&cobra.Group{ID: "project", Title: ui.Category("Project Commands:")},
		&cobra.Group{ID: "planning", Title: ui.Category("Planning Commands:")},
		&cobra.Group{ID: "global", Title: ui.Category("Global Commands:")},
		&cobra.Group{ID: "system", Title: ui.Category("System Commands:")},
	)

	// Global persistent flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.obey/campaign/config.json)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable verbose output")

	rootCmd.AddCommand(skillspkg.Cmd)
	rootCmd.AddCommand(cachepkg.Cmd)
	rootCmd.AddCommand(navigationpkg.Cmd)
	rootCmd.AddCommand(initcmd.New())
	rootCmd.AddCommand(navigationpkg.ShortcutsCmd)
	rootCmd.AddCommand(registrypkg.Cmd)
	rootCmd.AddCommand(projectpkg.Cmd)
	rootCmd.AddCommand(dungeonpkg.Cmd)
	rootCmd.AddCommand(intentpkg.Cmd)
	rootCmd.AddCommand(leveragepkg.Cmd)
	rootCmd.AddCommand(worktreespkg.Cmd)
	rootCmd.AddCommand(refspkg.Cmd)
	rootCmd.AddCommand(pluginsCmd)

	attachResolverFactory := func(stderr io.Writer, usageLine string) attachpkg.CampaignResolver {
		return attachpkg.NewResolver(stderr, usageLine)
	}
	rootCmd.AddCommand(attachpkg.NewAttachCommand(attachResolverFactory))
	rootCmd.AddCommand(attachpkg.NewDetachCommand())

	release.Register(rootCmd)
}

// styledLongDescription returns the styled long description for the root command.
func styledLongDescription() string {
	return `Camp manages multi-project AI workspaces with fast navigation.

Camp provides structure and navigation for AI-powered development workflows.
It creates standardized campaign directories, manages git submodules as projects,
and enables lightning-fast navigation through category shortcuts and TUI fuzzy finding.

` + ui.Category("GETTING STARTED:") + `
  ` + ui.Accent("camp init") + `               Initialize a new campaign in the current directory
  ` + ui.Accent("camp project list") + `       List all projects in the campaign
  ` + ui.Accent("camp list") + `               Show all registered campaigns

` + ui.Category("NAVIGATION (using cgo shell function):") + `
  ` + ui.Accent("cgo") + `                     Navigate to campaign root
  ` + ui.Accent("cgo p") + `                   Navigate to projects directory
  ` + ui.Accent("cgo f") + `                   Navigate to festivals directory
  ` + ui.Accent("cgo <name>") + `              Fuzzy find and navigate to any target

` + ui.Category("COMMON WORKFLOWS:") + `
  ` + ui.Accent("camp project add <url>") + `  Add a git repo as a project submodule
  ` + ui.Accent("camp run <command>") + `      Run command from campaign root directory
  ` + ui.Accent("camp shortcuts") + `          View all available navigation shortcuts

Run '` + ui.Accent("camp shell-init") + `' to enable the cgo navigation function.`
}

// styledHelpTemplate returns a styled help template for cobra commands.
func styledHelpTemplate() string {
	return `{{if .Long}}{{styleHelp .Long}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`
}

// styledUsageTemplate returns a styled usage template for cobra commands.
func styledUsageTemplate() string {
	return `{{styleCategory "Usage:"}}
  {{.UseLine}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

{{styleCategory "Aliases:"}}
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

{{styleCategory "Examples:"}}
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

{{styleCategory "Available Commands:"}}{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{styleAccent (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{styleAccent (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

{{styleCategory "Additional Commands:"}}{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{styleAccent (rpad .Name .NamePadding)}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

{{styleCategory "Flags:"}}
{{cleanFlagUsages .LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

{{styleCategory "Global Flags:"}}
{{cleanFlagUsages .InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

{{styleCategory "Additional help topics:"}}{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
}
