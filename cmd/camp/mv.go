package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/obediencecorp/camp/internal/campaign"
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
	RunE: runMove,
}

func init() {
	rootCmd.AddCommand(moveCmd)
	moveCmd.GroupID = "campaign"
	moveCmd.Flags().BoolP("force", "f", false, "Overwrite destination without prompting")
}

func runMove(cmd *cobra.Command, args []string) error {
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
