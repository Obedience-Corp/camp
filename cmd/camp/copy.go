package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/transfer"
	"github.com/spf13/cobra"
)

var copyCmd = &cobra.Command{
	Use:   "copy <src> <dest>",
	Short: "Copy a file or directory within the campaign",
	Long: `Copy a file or directory within the current campaign.

Both source and destination are resolved relative to the campaign root,
making it easy to copy things between campaign directories without
painful relative paths.

If the destination is an existing directory or ends with '/', the source
is placed inside it with the same basename. Directories are copied
recursively.`,
	Example: `  camp copy workflow/design/active/my-doc.md workflow/explore/my-doc.md
  camp cp festivals/active/my-fest/OVERVIEW.md docs/
  camp cp workflow/design/active/ workflow/explore/backup/`,
	Aliases: []string{"cp"},
	Args:    cobra.ExactArgs(2),
	RunE:    runCopy,
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

	src := transfer.ResolveCampaignRelative(root, args[0])
	dest := transfer.ResolveCampaignRelative(root, args[1])

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
