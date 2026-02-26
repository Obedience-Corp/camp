package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/transfer"
	"github.com/spf13/cobra"
)

var copyCmd = &cobra.Command{
	Use:   "copy <src> <dest>",
	Short: "Copy a file or directory within the campaign",
	Long: `Copy a file or directory within the current campaign.

Paths are resolved relative to the current directory, matching standard
'cp' behavior and tab completion.

Use @ prefix for campaign shortcuts (e.g., @p/fest, @f/active/).
Available shortcuts are defined in campaign config.

If the destination is an existing directory or ends with '/', the source
is placed inside it with the same basename. Directories are copied
recursively.`,
	Example: `  camp copy myfile.md ../docs/
  camp cp @f/active/my-fest/OVERVIEW.md @d/
  camp cp @w/design/active/ @w/explore/backup/`,
	Aliases:           []string{"cp"},
	Args:              cobra.ExactArgs(2),
	ValidArgsFunction: completeTransferArgs,
	RunE:              runCopy,
}

func init() {
	rootCmd.AddCommand(copyCmd)
	copyCmd.GroupID = "campaign"
	copyCmd.Flags().BoolP("force", "f", false, "Overwrite destination without prompting")
}

func runCopy(cmd *cobra.Command, args []string) error {
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

	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	if srcInfo.IsDir() {
		if err := transfer.CopyDir(src, dest); err != nil {
			return fmt.Errorf("copy directory: %w", err)
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("create destination directory: %w", err)
		}
		if err := transfer.CopyFile(src, dest); err != nil {
			return fmt.Errorf("copy file: %w", err)
		}
	}

	srcRel, _ := filepath.Rel(root, src)
	destRel, _ := filepath.Rel(root, dest)
	fmt.Printf("Copied %s → %s\n", srcRel, destRel)
	return nil
}
