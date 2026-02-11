package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/concept"
	"github.com/obediencecorp/camp/internal/transfer"
	"github.com/spf13/cobra"
)

var moveCmd = &cobra.Command{
	Use:   "move <src> <dest>",
	Short: "Move a file or directory within the campaign",
	Long: `Move a file or directory within the current campaign.

Both source and destination are resolved relative to the campaign root,
making it easy to move things between campaign directories without
painful relative paths.

If the destination is an existing directory or ends with '/', the source
is placed inside it with the same basename.`,
	Example: `  camp move workflow/design/active/my-doc.md workflow/explore/my-doc.md
  camp mv workflow/design/active/my-doc.md workflow/explore/
  camp mv festivals/active/old-fest festivals/completed/`,
	Aliases: []string{"mv"},
	Args:    cobra.ExactArgs(2),
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Interactive project picker in no-arg mode",
		"interactive":   "true",
	},
	ValidArgsFunction: completeMoveArgs,
	RunE:              runMove,
}

func init() {
	rootCmd.AddCommand(moveCmd)
	moveCmd.GroupID = "campaign"
	moveCmd.Flags().BoolP("force", "f", false, "Overwrite destination without prompting")
}

// completeMoveArgs provides tab completion for camp mv arguments with @ prefix support.
func completeMoveArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) >= 2 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// If starts with @, resolve and complete
	if strings.HasPrefix(toComplete, "@") {
		return completeAtPath(campRoot, toComplete)
	}

	// Default: filesystem completion relative to campaign root
	return nil, cobra.ShellCompDirectiveDefault
}

// completeAtPath provides completions for @-prefixed paths.
func completeAtPath(campRoot, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Try to resolve the prefix
	resolved, err := concept.ResolveAtPath(toComplete)
	if err != nil {
		// Maybe partial @ prefix — offer all @ shortcuts
		var completions []string
		for prefix, dir := range concept.DefaultAtPrefixes {
			if strings.HasPrefix(prefix, toComplete) {
				completions = append(completions, prefix+"/\t"+dir)
			}
		}
		return completions, cobra.ShellCompDirectiveNoSpace
	}

	// List entries in the resolved directory
	fullPath := filepath.Join(campRoot, resolved)
	dir := fullPath
	partial := ""

	info, err := os.Stat(fullPath)
	if err != nil || !info.IsDir() {
		// The resolved path might be a partial — split into dir + partial
		dir = filepath.Dir(fullPath)
		partial = filepath.Base(fullPath)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	// Extract the @ prefix portion to reconstruct completions
	atPrefix := toComplete
	if idx := strings.IndexByte(toComplete, '/'); idx != -1 {
		atPrefix = toComplete[:idx]
	}

	// Build the relative path within the concept dir
	resolvedBase, _ := concept.ResolveAtPath(atPrefix + "/")
	subpath := ""
	if len(resolved) > len(resolvedBase) {
		subpath = resolved[len(resolvedBase)+1:]
		// Get the directory portion of subpath
		if partial != "" && partial != filepath.Base(resolvedBase) {
			subpath = filepath.Dir(subpath)
			if subpath == "." {
				subpath = ""
			}
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
		completion := atPrefix + "/"
		if subpath != "" {
			completion += subpath + "/"
		}
		completion += name
		if entry.IsDir() {
			completion += "/"
		}
		completions = append(completions, completion)
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

	// Resolve @ prefix shortcuts before path resolution
	srcArg, err := concept.ResolveAtPath(args[0])
	if err != nil {
		return err
	}
	destArg, err := concept.ResolveAtPath(args[1])
	if err != nil {
		return err
	}

	src := transfer.ResolveCampaignRelative(root, srcArg)
	dest := transfer.ResolveCampaignRelative(root, destArg)

	if err := transfer.ValidatePathExists(src); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	// If dest is a directory or ends with /, place source inside it
	if transfer.IsDestDir(dest) || transfer.IsDestDir(destArg) {
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
