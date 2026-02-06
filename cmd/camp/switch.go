package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/git"
	"github.com/spf13/cobra"
)

var switchCmd = &cobra.Command{
	Use:   "switch <project>",
	Short: "Switch to a project directory",
	Long: `Switch to a project (submodule) directory by name.

Supports fuzzy matching — partial names match if unambiguous.
Use with the cgo shell function for instant navigation:
  cgo switch fest      # cd to projects/fest

The --print flag outputs just the path for shell integration:
  cd "$(camp switch fest --print)"`,
	Example: `  camp switch fest           # Switch to fest project
  camp switch obey           # Switch to obey-* (if unambiguous)
  camp switch camp --print   # Print path only`,
	Aliases: []string{"sw"},
	Args:    cobra.ExactArgs(1),
	RunE:    runSwitch,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx := cmd.Context()
		root, err := campaign.DetectCached(ctx)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		matches, err := git.ListSubmodulePathsFiltered(ctx, root, toComplete)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		// Return just the directory name (last segment of submodule path)
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, filepath.Base(m))
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	},
}

func init() {
	rootCmd.AddCommand(switchCmd)
	switchCmd.GroupID = "navigation"
	switchCmd.Flags().Bool("print", false, "Print path only (for shell integration)")
}

func runSwitch(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	printOnly, _ := cmd.Flags().GetBool("print")

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	query := args[0]
	match, err := matchProject(ctx, root, query)
	if err != nil {
		return err
	}

	if printOnly {
		fmt.Println(match)
	} else {
		fmt.Printf("cd %s\n", match)
	}
	return nil
}

// matchProject finds a project by name using exact, prefix, then substring matching.
// Returns the absolute path to the matched project.
func matchProject(ctx context.Context, campRoot, query string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	all, err := git.ListSubmodulePaths(ctx, campRoot)
	if err != nil {
		return "", fmt.Errorf("list submodules: %w", err)
	}
	if len(all) == 0 {
		return "", fmt.Errorf("no submodules found in campaign")
	}

	query = strings.ToLower(query)

	// Phase 1: Exact match on directory name
	for _, p := range all {
		name := strings.ToLower(filepath.Base(p))
		if name == query {
			return filepath.Join(campRoot, p), nil
		}
	}

	// Phase 2: Prefix match
	var prefixMatches []string
	for _, p := range all {
		name := strings.ToLower(filepath.Base(p))
		if strings.HasPrefix(name, query) {
			prefixMatches = append(prefixMatches, p)
		}
	}
	if len(prefixMatches) == 1 {
		return filepath.Join(campRoot, prefixMatches[0]), nil
	}
	if len(prefixMatches) > 1 {
		return "", ambiguousMatchError(query, prefixMatches)
	}

	// Phase 3: Substring match
	var substringMatches []string
	for _, p := range all {
		name := strings.ToLower(filepath.Base(p))
		if strings.Contains(name, query) {
			substringMatches = append(substringMatches, p)
		}
	}
	if len(substringMatches) == 1 {
		return filepath.Join(campRoot, substringMatches[0]), nil
	}
	if len(substringMatches) > 1 {
		return "", ambiguousMatchError(query, substringMatches)
	}

	return "", fmt.Errorf("no project matching %q found", query)
}

func ambiguousMatchError(query string, matches []string) error {
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = filepath.Base(m)
	}
	return fmt.Errorf("ambiguous match for %q: %s", query, strings.Join(names, ", "))
}
