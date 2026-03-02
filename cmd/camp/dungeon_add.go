package main

import (
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var dungeonAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Initialize dungeon structure",
	Long: `Initialize the dungeon directory with documentation and structure.

Creates the dungeon directory with:
  - OBEY.md: Documentation explaining the dungeon's purpose
  - completed/: Successfully finished work
  - archived/: Preserved for history, truly done
  - someday/: Low priority, might revisit

This creates the same dungeon structure as 'camp flow init' but without
the full workflow (no .workflow.yaml, active/, or ready/ directories).
Useful when you only need a dungeon for idea capture or temporary holding.

This operation is idempotent - running it multiple times is safe.
Use --force to overwrite existing files.

Examples:
  camp dungeon add          Initialize dungeon (skip existing files)
  camp dungeon add --force  Overwrite existing documentation`,
	Args: cobra.NoArgs,
	RunE: runDungeonAdd,
}

func init() {
	dungeonCmd.AddCommand(dungeonAddCmd)

	flags := dungeonAddCmd.Flags()
	flags.BoolP("force", "f", false, "Overwrite existing files")
}

func runDungeonAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	force, _ := cmd.Flags().GetBool("force")

	// Load campaign config
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	// Get current working directory for local dungeon
	cwd, err := os.Getwd()
	if err != nil {
		return camperrors.Wrap(err, "getting current directory")
	}
	dungeonPath := filepath.Join(cwd, "dungeon")

	// Create dungeon service (still validates campaign, but uses CWD for dungeon)
	_ = cfg // campaign config loaded to validate we're in a campaign
	svc := dungeon.NewService(campaignRoot, dungeonPath)

	// Initialize dungeon
	result, err := svc.Init(ctx, dungeon.InitOptions{
		Force: force,
	})
	if err != nil {
		return camperrors.Wrap(err, "initializing dungeon")
	}

	// Report results
	relPath := func(p string) string {
		rel, err := filepath.Rel(cwd, p)
		if err != nil {
			return p
		}
		return rel
	}

	if len(result.CreatedDirs) > 0 {
		for _, dir := range result.CreatedDirs {
			fmt.Printf("%s Created directory: %s\n", ui.SuccessIcon(), ui.Value(relPath(dir)))
		}
	}

	if len(result.CreatedFiles) > 0 {
		for _, file := range result.CreatedFiles {
			fmt.Printf("%s Created file: %s\n", ui.SuccessIcon(), ui.Value(relPath(file)))
		}
	}

	if len(result.Skipped) > 0 {
		for _, file := range result.Skipped {
			fmt.Printf("%s Skipped (exists): %s\n", ui.BulletIcon(), ui.Dim(relPath(file)))
		}
	}

	// Summary
	totalCreated := len(result.CreatedDirs) + len(result.CreatedFiles)
	if totalCreated == 0 && len(result.Skipped) > 0 {
		fmt.Printf("\n%s Dungeon already initialized. Use --force to overwrite.\n", ui.InfoIcon())
	} else if totalCreated > 0 {
		fmt.Printf("\n%s Dungeon initialized at %s\n", ui.SuccessIcon(), ui.Value("./dungeon"))
	}

	return nil
}
