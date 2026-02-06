package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/git"
	"github.com/obediencecorp/camp/internal/transfer"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var mvCmd = &cobra.Command{
	Use:   "mv <file> [project]",
	Short: "Move a file to the same path in another project",
	Long: `Move a file to the same relative path in a different campaign project.

If the target project is omitted and stdin is a TTY, an interactive
project picker is displayed.

The file's relative path within the current project is preserved in
the destination. For example, moving internal/foo.go to project "beta"
places it at beta/internal/foo.go.`,
	Example: `  camp mv internal/foo.go obey-daemon  # Move to same path in obey-daemon
  camp mv cmd/main.go                  # Interactive project picker`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runMv,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return nil, cobra.ShellCompDirectiveDefault
		}
		if len(args) == 1 {
			ctx := cmd.Context()
			root, err := campaign.DetectCached(ctx)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			matches, err := git.ListSubmodulePathsFiltered(ctx, root, toComplete)
			if err != nil {
				return nil, cobra.ShellCompDirectiveError
			}
			names := make([]string, 0, len(matches))
			for _, m := range matches {
				names = append(names, filepath.Base(m))
			}
			return names, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	rootCmd.AddCommand(mvCmd)
	mvCmd.GroupID = "project"
	mvCmd.Flags().BoolP("force", "f", false, "Overwrite destination without prompting")
}

func runMv(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	force, _ := cmd.Flags().GetBool("force")

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	file := args[0]
	var targetProject string

	if len(args) >= 2 {
		targetProject = args[1]
	} else {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("no target project specified (usage: camp mv <file> <project>)")
		}
		selected, err := pickTargetProject(ctx, root)
		if err != nil {
			return err
		}
		targetProject = selected
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	src, dest, err := transfer.ResolveShortcut(ctx, root, cwd, file, targetProject)
	if err != nil {
		return err
	}

	if err := transfer.ValidatePathExists(src); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	if !force {
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("destination %q already exists (use --force to overwrite)", dest)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	if err := os.Rename(src, dest); err != nil {
		return fmt.Errorf("move file: %w", err)
	}

	fmt.Printf("Moved %s → %s:%s\n", file, targetProject, filepath.Base(dest))
	return nil
}

// pickTargetProject shows an interactive project selector using huh.
func pickTargetProject(ctx context.Context, root string) (string, error) {
	all, err := git.ListSubmodulePaths(ctx, root)
	if err != nil {
		return "", fmt.Errorf("list projects: %w", err)
	}
	if len(all) == 0 {
		return "", fmt.Errorf("no projects found in campaign")
	}

	options := make([]huh.Option[string], 0, len(all))
	for _, p := range all {
		name := filepath.Base(p)
		options = append(options, huh.NewOption(name, name))
	}

	var selected string
	err = huh.NewSelect[string]().
		Title("Select target project").
		Options(options...).
		Value(&selected).
		Run()
	if err != nil {
		return "", fmt.Errorf("project selection: %w", err)
	}
	return selected, nil
}
