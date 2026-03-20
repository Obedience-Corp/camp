package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/plugin"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// errPluginHandled is a sentinel indicating a plugin ran successfully.
// Execute() converts this to nil so main() sees a clean exit.
var errPluginHandled = errors.New("plugin handled")

// dispatchPlugin checks whether the first non-flag argument is an unknown
// subcommand backed by a camp-<name> binary on PATH. If so, it executes
// the plugin and returns errPluginHandled (success) or a CommandError.
// Returns nil to fall through to Cobra's normal dispatch.
func dispatchPlugin() error {
	name, argIdx := firstSubcommand()
	if name == "" {
		return nil
	}

	// Never intercept help, completion, or Cobra internals.
	switch name {
	case "help", "completion", "__complete", "__completeNoDesc":
		return nil
	}

	if isKnownCommand(name) {
		return nil
	}

	p, found := plugin.Lookup(name)
	if !found {
		return nil
	}

	// Best-effort campaign root detection for the plugin environment.
	ctx := context.Background()
	campRoot, _ := campaign.DetectCached(ctx)

	// Forward all args after the plugin subcommand name.
	var pluginArgs []string
	if argIdx+1 < len(os.Args) {
		pluginArgs = os.Args[argIdx+1:]
	}

	if err := plugin.Execute(ctx, p, pluginArgs, campRoot); err != nil {
		return err
	}
	return errPluginHandled
}

// firstSubcommand returns the first non-flag argument from os.Args and its
// index. Returns ("", 0) if no subcommand is present.
//
// It consults the root command's persistent flags so that flags which take
// values (e.g. --config <file>) have their value skipped rather than being
// mistaken for a subcommand name.
func firstSubcommand() (string, int) {
	return findFirstPositionalArg(os.Args)
}

// findFirstPositionalArg scans args (where args[0] is the program name) and
// returns the first positional (non-flag) argument and its index.
// It correctly skips values consumed by flags like --config <file> and
// handles --flag=value, --, and boolean flags.
func findFirstPositionalArg(args []string) (string, int) {
	if len(args) < 2 {
		return "", 0
	}

	pflags := rootCmd.PersistentFlags()

	for i := 1; i < len(args); i++ {
		arg := args[i]

		// "--" terminates flag parsing; the next arg is the first positional.
		if arg == "--" {
			if i+1 < len(args) {
				return args[i+1], i + 1
			}
			return "", 0
		}

		if len(arg) == 0 || arg[0] != '-' {
			return arg, i
		}

		// It's a flag. Determine whether it consumes the next argument.
		// Flags using --flag=value syntax never consume the next arg.
		if strings.Contains(arg, "=") {
			continue
		}

		// Strip leading dashes to get the flag name.
		name := strings.TrimLeft(arg, "-")

		// Look up the flag in persistent flags to check if it takes a value.
		if f := pflags.Lookup(name); f != nil && f.NoOptDefVal == "" {
			// Flag takes a value — skip the next argument.
			i++
			continue
		}
	}
	return "", 0
}

// isKnownCommand reports whether name matches a registered Cobra subcommand
// (by name or alias) or a Cobra built-in like "help".
func isKnownCommand(name string) bool {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == name {
			return true
		}
		if slices.Contains(cmd.Aliases, name) {
			return true
		}
	}
	return false
}

var pluginsCmd = &cobra.Command{
	Use:     "plugins",
	Short:   "List discovered camp plugins on PATH",
	GroupID: "system",
	RunE: func(cmd *cobra.Command, args []string) error {
		plugins, err := plugin.Discover(cmd.Context())
		if err != nil {
			return err
		}

		if len(plugins) == 0 {
			fmt.Println("No camp plugins found on PATH.")
			fmt.Println("Install a plugin binary named camp-<name> to extend camp.")
			return nil
		}

		fmt.Println(ui.Category("Installed Plugins:"))
		for _, p := range plugins {
			fmt.Printf("  %-20s %s\n", ui.Accent(p.Name), p.Path)
		}
		return nil
	},
}
