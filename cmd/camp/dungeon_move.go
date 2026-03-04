package main

import (
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/ui"
)

var dungeonMoveCmd = &cobra.Command{
	Use:   "move <item> [status]",
	Short: "Move dungeon items between statuses",
	Long: `Move items within the dungeon or from the parent directory into the dungeon.

Without --triage, moves an item already in the dungeon root to a status directory.
With --triage, moves an item from the parent directory into the dungeon.

Statuses: completed, archived, someday

Examples:
  camp dungeon move old-feature archived         Move dungeon item to archived
  camp dungeon move stale-doc completed          Move dungeon item to completed
  camp dungeon move old-project --triage         Move parent item into dungeon root
  camp dungeon move old-project archived --triage Move parent item directly to archived`,
	Args: cobra.RangeArgs(1, 2),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive move of dungeon items between statuses",
	},
	RunE: runDungeonMove,
}

func init() {
	dungeonCmd.AddCommand(dungeonMoveCmd)

	flags := dungeonMoveCmd.Flags()
	flags.Bool("triage", false, "Move from parent directory (not from dungeon root)")
	flags.Bool("no-commit", false, "Don't create a git commit")
}

func runDungeonMove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	triageMode, _ := cmd.Flags().GetBool("triage")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	itemName := args[0]
	status := ""
	if len(args) > 1 {
		status = args[1]
	}

	// Load campaign config
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign directory")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return camperrors.Wrap(err, "getting current directory")
	}
	dungeonPath := filepath.Join(cwd, "dungeon")

	svc := dungeon.NewService(campaignRoot, dungeonPath)

	var description string
	var movedPaths []string

	if triageMode {
		if status != "" {
			// Triage directly to a status directory
			if err := svc.MoveToDungeonStatus(ctx, itemName, cwd, status); err != nil {
				return camperrors.Wrapf(err, "moving %s to dungeon/%s", itemName, status)
			}
			fmt.Printf("%s Moved %s → dungeon/%s/\n", ui.SuccessIcon(), itemName, status)
			description = fmt.Sprintf("Triage %s → dungeon/%s", itemName, status)
			movedPaths = []string{
				filepath.Join(cwd, itemName),
				filepath.Join(cwd, "dungeon", status, itemName),
			}
		} else {
			// Triage to dungeon root
			if err := svc.MoveToDungeon(ctx, itemName, cwd); err != nil {
				return camperrors.Wrapf(err, "moving %s to dungeon", itemName)
			}
			fmt.Printf("%s Moved %s → dungeon/\n", ui.SuccessIcon(), itemName)
			description = fmt.Sprintf("Triage %s → dungeon", itemName)
			movedPaths = []string{
				filepath.Join(cwd, itemName),
				filepath.Join(cwd, "dungeon", itemName),
			}
		}
	} else {
		// Inner move: dungeon root → status directory
		if status == "" {
			return fmt.Errorf("status is required when moving within the dungeon (e.g., completed, archived, someday)")
		}
		if err := svc.MoveToStatus(ctx, itemName, status); err != nil {
			return camperrors.Wrapf(err, "moving %s to %s", itemName, status)
		}
		fmt.Printf("%s Moved %s → dungeon/%s/\n", ui.SuccessIcon(), itemName, status)

		relDir, relErr := filepath.Rel(campaignRoot, cwd)
		if relErr != nil {
			relDir = cwd
		}
		description = fmt.Sprintf("Moved to dungeon/%s:\n  - %s/%s", status, relDir, itemName)
		movedPaths = []string{
			filepath.Join(cwd, "dungeon", itemName),
			filepath.Join(cwd, "dungeon", status, itemName),
		}
	}

	// Auto-commit
	if !noCommit {
		files := commit.NormalizeFiles(campaignRoot, movedPaths...)
		result := commit.Crawl(ctx, commit.CrawlOptions{
			Options: commit.Options{
				CampaignRoot: campaignRoot,
				CampaignID:   cfg.ID,
			},
			Description: strings.TrimSpace(description),
			Files:       files,
		})
		if result.Committed {
			fmt.Printf("%s %s\n", ui.SuccessIcon(), result.Message)
		} else if result.Message != "" {
			fmt.Printf("%s %s\n", ui.InfoIcon(), result.Message)
		}
	}

	return nil
}
