package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/transfer"
	"github.com/spf13/cobra"
)

var transferCmd = &cobra.Command{
	Use:   "transfer <src> <dest>",
	Short: "Copy files between campaigns",
	Long: `Copy files between different campaigns using campaign:path syntax.

Transfer always copies — it never moves or deletes the source.
Either the source or destination (or both) can use "campaign:path"
notation to reference a different registered campaign. Paths without
a campaign prefix resolve relative to the current campaign root.

At least one side must reference a different campaign. For copies
within the same campaign, use 'camp copy' instead.`,
	Example: `  camp transfer docs/my-doc.md other-campaign:docs/my-doc.md     # push
  camp transfer other-campaign:docs/my-doc.md docs/              # pull
  camp transfer other:festivals/plan.md festivals/planned/       # pull into dir`,
	Args: cobra.ExactArgs(2),
	RunE: runTransfer,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 2 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeTransferArg(cmd, toComplete)
	},
}

func init() {
	rootCmd.AddCommand(transferCmd)
	transferCmd.GroupID = "global"
	transferCmd.Flags().BoolP("force", "f", false, "Overwrite destination without prompting")
}

func runTransfer(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	force, _ := cmd.Flags().GetBool("force")

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	srcPath, err := transfer.ResolveCrossCampaignPath(ctx, root, args[0])
	if err != nil {
		return fmt.Errorf("resolve source: %w", err)
	}
	if err := transfer.ValidatePathExists(srcPath); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	destPath, err := transfer.ResolveCrossCampaignPath(ctx, root, args[1])
	if err != nil {
		return fmt.Errorf("resolve destination: %w", err)
	}

	// If dest is a directory or ends with /, place source inside it
	destArg := args[1]
	// Strip campaign prefix for trailing slash check
	if idx := strings.Index(destArg, ":"); idx >= 0 {
		destArg = destArg[idx+1:]
	}
	if transfer.IsDestDir(destPath) || transfer.IsDestDir(destArg) {
		destPath = filepath.Join(destPath, filepath.Base(srcPath))
	}

	if !force {
		if _, err := os.Stat(destPath); err == nil {
			return fmt.Errorf("destination %q already exists (use --force to overwrite)", destPath)
		}
	}

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	if srcInfo.IsDir() {
		if err := transfer.CopyDir(srcPath, destPath); err != nil {
			return fmt.Errorf("transfer directory: %w", err)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("create destination directory: %w", err)
		}
		if err := transfer.CopyFile(srcPath, destPath); err != nil {
			return fmt.Errorf("transfer file: %w", err)
		}
	}

	fmt.Printf("Transferred %s → %s\n", args[0], args[1])
	return nil
}

// completeTransferArg provides tab completion for campaign:path arguments.
func completeTransferArg(cmd *cobra.Command, toComplete string) ([]string, cobra.ShellCompDirective) {
	ctx := cmd.Context()

	// Check if we're completing after the colon (file paths within a campaign)
	if idx := strings.Index(toComplete, ":"); idx >= 0 {
		campaignName := toComplete[:idx]
		pathPrefix := toComplete[idx+1:]

		reg, err := config.LoadRegistry(ctx)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		entry, ok := reg.Get(campaignName)
		if !ok {
			return nil, cobra.ShellCompDirectiveError
		}

		return completeCampaignPath(entry.Path, pathPrefix, toComplete[:idx+1])
	}

	// Before colon: complete campaign names with trailing colon
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	toCompleteLower := strings.ToLower(toComplete)
	var names []string
	for _, c := range reg.ListAll() {
		if strings.HasPrefix(strings.ToLower(c.Name), toCompleteLower) {
			names = append(names, c.Name+":")
		}
	}
	return names, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}

// completeCampaignPath completes file paths within a campaign directory.
func completeCampaignPath(campRoot, pathPrefix, colonPrefix string) ([]string, cobra.ShellCompDirective) {
	searchDir := filepath.Join(campRoot, pathPrefix)
	dirToRead := searchDir
	prefix := pathPrefix

	var filter string
	if pathPrefix != "" && !strings.HasSuffix(pathPrefix, "/") {
		dirToRead = filepath.Dir(searchDir)
		filter = filepath.Base(pathPrefix)
		prefix = filepath.Dir(pathPrefix)
		if prefix == "." {
			prefix = ""
		} else {
			prefix += "/"
		}
	}

	entries, err := os.ReadDir(dirToRead)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var completions []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if filter != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(filter)) {
			continue
		}
		suffix := ""
		if e.IsDir() {
			suffix = "/"
		}
		completions = append(completions, colonPrefix+prefix+name+suffix)
	}
	return completions, cobra.ShellCompDirectiveNoSpace | cobra.ShellCompDirectiveNoFileComp
}
