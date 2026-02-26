package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/transfer"
	"github.com/spf13/cobra"
)

var moveCmd = &cobra.Command{
	Use:   "move <src> <dest>",
	Short: "Move a file or directory within the campaign",
	Long: `Move a file or directory within the current campaign.

Paths are resolved relative to the current directory, matching standard
'mv' behavior and tab completion.

Use @ prefix for campaign shortcuts (e.g., @p/fest, @f/active/).
Available shortcuts are defined in campaign config.

If the destination is an existing directory or ends with '/', the source
is placed inside it with the same basename.`,
	Example: `  camp move mydir/ ../docs/mydir/
  camp mv @f/active/old-fest @f/completed/
  camp mv draft.md @w/design/`,
	Aliases: []string{"mv"},
	Args:    cobra.ExactArgs(2),
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Interactive project picker in no-arg mode",
		"interactive":   "true",
	},
	ValidArgsFunction: completeTransferArgs,
	RunE:              runMove,
}

func init() {
	rootCmd.AddCommand(moveCmd)
	moveCmd.GroupID = "campaign"
	moveCmd.Flags().BoolP("force", "f", false, "Overwrite destination without prompting")
}

// completeTransferArgs provides tab completion for camp mv/cp arguments with @ prefix support.
func completeTransferArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) >= 2 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// If starts with @, complete against campaign shortcuts
	if strings.HasPrefix(toComplete, "@") {
		campRoot, err := campaign.DetectCached(ctx)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		cfg, err := config.LoadCampaignConfig(ctx, campRoot)
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		shortcuts := buildShortcutsMap(cfg)
		return completeAtPath(campRoot, toComplete, shortcuts)
	}

	// Default: filesystem completion relative to cwd
	return nil, cobra.ShellCompDirectiveDefault
}

// completeAtPath provides completions for @-prefixed paths using campaign shortcuts.
func completeAtPath(campRoot, toComplete string, shortcuts map[string]string) ([]string, cobra.ShellCompDirective) {
	query := toComplete[1:] // strip leading @

	slashIdx := strings.IndexByte(query, '/')
	if slashIdx < 0 {
		// No slash yet: offer matching shortcut keys
		var completions []string
		for key, dir := range shortcuts {
			if strings.HasPrefix(key, query) {
				completions = append(completions, "@"+key+"/\t"+dir)
			}
		}
		return completions, cobra.ShellCompDirectiveNoSpace
	}

	// Has slash: resolve the key and list directory entries
	key := query[:slashIdx]
	dir, ok := shortcuts[key]
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	rest := query[slashIdx+1:]
	fullPath := filepath.Join(campRoot, dir, rest)

	dirToList := fullPath
	partial := ""
	info, err := os.Stat(fullPath)
	if err != nil || !info.IsDir() {
		dirToList = filepath.Dir(fullPath)
		partial = filepath.Base(fullPath)
	}

	entries, err := os.ReadDir(dirToList)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Build completion prefix: @key/ + subdirectory path within the shortcut dir
	prefix := "@" + key + "/"
	if rest != "" {
		subDir := rest
		if partial != "" {
			subDir = filepath.Dir(rest)
			if subDir == "." {
				subDir = ""
			}
		}
		if subDir != "" {
			prefix += subDir + "/"
		}
	}

	var completions []string
	lowerPartial := strings.ToLower(partial)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if partial != "" && !strings.HasPrefix(strings.ToLower(name), lowerPartial) {
			continue
		}
		comp := prefix + name
		if entry.IsDir() {
			comp += "/"
		}
		completions = append(completions, comp)
	}

	return completions, cobra.ShellCompDirectiveNoSpace
}

func runMove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	force, _ := cmd.Flags().GetBool("force")

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	// Load campaign config for @ shortcut resolution
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return err
	}
	shortcuts := buildShortcutsMap(cfg)

	// Resolve paths: @ prefix -> campaign shortcuts, otherwise -> cwd-relative
	src, err := resolveTransferArg(root, args[0], shortcuts)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	dest, err := resolveTransferArg(root, args[1], shortcuts)
	if err != nil {
		return fmt.Errorf("destination: %w", err)
	}

	if err := transfer.ValidatePathExists(src); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	// If dest is a directory or ends with /, place source inside it
	if transfer.IsDestDir(dest) || transfer.IsDestDir(args[1]) {
		dest = filepath.Join(dest, filepath.Base(src))
	}

	if !force {
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("destination %q already exists (use --force to overwrite)", dest)
		}
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	if err := os.Rename(src, dest); err != nil {
		return fmt.Errorf("move: %w", err)
	}

	srcRel, _ := filepath.Rel(root, src)
	destRel, _ := filepath.Rel(root, dest)
	fmt.Printf("Moved %s → %s\n", srcRel, destRel)
	return nil
}
