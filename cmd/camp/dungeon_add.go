package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/dungeon"
	"github.com/obediencecorp/camp/internal/paths"
	"github.com/obediencecorp/camp/internal/ui"
)

var dungeonAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Initialize dungeon structure",
	Long: `Initialize the dungeon directory with documentation and structure.

Creates the dungeon directory with:
  - OBEY.md: Documentation explaining the dungeon's purpose
  - archived/: Directory for truly archived items
  - archived/README.md: Instructions for recovery

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
		return fmt.Errorf("not in a campaign directory: %w", err)
	}

	// Create path resolver
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)

	// Create dungeon service
	svc := dungeon.NewService(campaignRoot, resolver.Dungeon())

	// Initialize dungeon
	result, err := svc.Init(ctx, dungeon.InitOptions{
		Force: force,
	})
	if err != nil {
		return fmt.Errorf("initializing dungeon: %w", err)
	}

	// Report results
	relPath := func(p string) string {
		rel, err := filepath.Rel(campaignRoot, p)
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
		fmt.Printf("\n%s Dungeon initialized at %s\n", ui.SuccessIcon(), ui.Value(relPath(resolver.Dungeon())))
	}

	return nil
}
