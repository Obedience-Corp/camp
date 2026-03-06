package main

import (
	"errors"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

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
With --triage and --to-docs, routes an item to an existing campaign-root docs/<subdirectory>.

Statuses: completed, archived, someday

Examples:
  camp dungeon move old-feature archived         Move dungeon item to archived
  camp dungeon move stale-doc completed          Move dungeon item to completed
  camp dungeon move old-project --triage         Move parent item into dungeon root
  camp dungeon move old-project archived --triage Move parent item directly to archived
  camp dungeon move stale-note.md --triage --to-docs architecture/api Route to docs subdirectory`,
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
	flags.String("to-docs", "", "Route triage item into an existing campaign-root docs/<subdir> (requires --triage)")
	flags.Bool("no-commit", false, "Don't create a git commit")
}

func runDungeonMove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	triageMode, _ := cmd.Flags().GetBool("triage")
	toDocs, _ := cmd.Flags().GetString("to-docs")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	itemName := args[0]
	status := ""
	if len(args) > 1 {
		status = args[1]
	}

	if toDocs != "" {
		if !triageMode {
			return fmt.Errorf("--to-docs requires --triage because docs routing moves parent triage items")
		}
		if status != "" {
			return fmt.Errorf("status argument cannot be combined with --to-docs; use either a dungeon status or --to-docs <subdir>")
		}
	}

	cmdCtx, err := resolveDungeonCommandContext(ctx)
	if err != nil {
		return err
	}
	svc := dungeon.NewService(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath)

	var description string
	var movedPaths []string

	if triageMode {
		if toDocs != "" {
			targetPath, err := svc.MoveToDocs(ctx, itemName, cmdCtx.Dungeon.ParentPath, toDocs)
			if err != nil {
				return wrapDungeonDocsRouteError(err, itemName, toDocs)
			}
			src := filepath.Join(relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath), itemName)
			dst := relFromRoot(cmdCtx.CampaignRoot, targetPath)
			fmt.Printf("%s Moved %s (%s → %s)\n", ui.SuccessIcon(), itemName, src, dst)
			description = fmt.Sprintf("Route %s → %s", itemName, dst)
			movedPaths = []string{
				filepath.Join(cmdCtx.Dungeon.ParentPath, itemName),
				targetPath,
			}
		} else if status != "" {
			// Triage directly to a status directory
			if err := svc.MoveToDungeonStatus(ctx, itemName, cmdCtx.Dungeon.ParentPath, status); err != nil {
				return wrapDungeonMoveError(err, itemName, status)
			}
			src := filepath.Join(relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath), itemName)
			dst := filepath.Join(relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath), status, itemName)
			fmt.Printf("%s Moved %s (%s → %s)\n", ui.SuccessIcon(), itemName, src, dst)
			description = fmt.Sprintf("Triage %s → dungeon/%s", itemName, status)
			movedPaths = []string{
				filepath.Join(cmdCtx.Dungeon.ParentPath, itemName),
				filepath.Join(cmdCtx.Dungeon.DungeonPath, status, itemName),
			}
		} else {
			// Triage to dungeon root
			if err := svc.MoveToDungeon(ctx, itemName, cmdCtx.Dungeon.ParentPath); err != nil {
				return wrapDungeonMoveError(err, itemName, "dungeon")
			}
			src := filepath.Join(relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath), itemName)
			dst := filepath.Join(relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath), itemName)
			fmt.Printf("%s Moved %s (%s → %s)\n", ui.SuccessIcon(), itemName, src, dst)
			description = fmt.Sprintf("Triage %s → dungeon", itemName)
			movedPaths = []string{
				filepath.Join(cmdCtx.Dungeon.ParentPath, itemName),
				filepath.Join(cmdCtx.Dungeon.DungeonPath, itemName),
			}
		}
	} else {
		// Inner move: dungeon root → status directory
		if status == "" {
			return fmt.Errorf("status is required when moving within the dungeon (e.g., completed, archived, someday)")
		}
		if err := svc.MoveToStatus(ctx, itemName, status); err != nil {
			return wrapDungeonMoveError(err, itemName, status)
		}
		src := filepath.Join(relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath), itemName)
		dst := filepath.Join(relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath), status, itemName)
		fmt.Printf("%s Moved %s (%s → %s)\n", ui.SuccessIcon(), itemName, src, dst)

		relDir, relErr := filepath.Rel(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath)
		if relErr != nil {
			relDir = cmdCtx.Dungeon.ParentPath
		}
		description = fmt.Sprintf("Moved to dungeon/%s:\n  - %s/%s", status, relDir, itemName)
		movedPaths = []string{
			filepath.Join(cmdCtx.Dungeon.DungeonPath, itemName),
			filepath.Join(cmdCtx.Dungeon.DungeonPath, status, itemName),
		}
	}

	// Auto-commit
	if !noCommit {
		files := commit.NormalizeFiles(cmdCtx.CampaignRoot, movedPaths...)
		result := commit.Crawl(ctx, commit.CrawlOptions{
			Options: commit.Options{
				CampaignRoot: cmdCtx.CampaignRoot,
				CampaignID:   cmdCtx.Config.ID,
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

func wrapDungeonMoveError(err error, itemName, status string) error {
	switch {
	case errors.Is(err, dungeon.ErrAlreadyExists):
		return fmt.Errorf(
			"cannot move %q to %q because destination already exists; choose another status or rename the item: %w",
			itemName,
			status,
			err,
		)
	case errors.Is(err, dungeon.ErrInvalidStatus):
		return fmt.Errorf(
			"invalid status %q for %q; use a single directory name like completed, archived, or someday: %w",
			status,
			itemName,
			err,
		)
	case errors.Is(err, dungeon.ErrInvalidItemPath):
		return fmt.Errorf(
			"invalid item path %q; use a direct child file or directory name from the current dungeon context (no slashes or traversal). Run 'camp dungeon list --triage' or 'camp dungeon list' to confirm available items: %w",
			itemName,
			err,
		)
	case errors.Is(err, dungeon.ErrNotFound):
		return fmt.Errorf(
			"item %q was not found in the resolved context; run 'camp dungeon list --triage' or 'camp dungeon list' to confirm available items: %w",
			itemName,
			err,
		)
	default:
		return camperrors.Wrapf(err, "moving %s to %s", itemName, status)
	}
}

func wrapDungeonDocsRouteError(err error, itemName, destination string) error {
	switch {
	case errors.Is(err, dungeon.ErrAlreadyExists):
		return fmt.Errorf(
			"cannot route %q to docs/%s because destination already exists; choose another docs destination or rename the item: %w",
			itemName,
			destination,
			err,
		)
	case errors.Is(err, dungeon.ErrInvalidDocsDestination):
		return fmt.Errorf(
			"invalid docs destination %q; use an existing subdirectory under campaign-root docs/ (for example: --to-docs architecture/api): %w",
			destination,
			err,
		)
	case errors.Is(err, dungeon.ErrInvalidItemPath):
		return fmt.Errorf(
			"invalid item path %q; use a direct child file or directory name from the resolved triage context (no slashes or traversal). Run 'camp dungeon list --triage' to confirm available items: %w",
			itemName,
			err,
		)
	case errors.Is(err, dungeon.ErrNotFound):
		return fmt.Errorf(
			"item %q was not found in the resolved triage context; run 'camp dungeon list --triage' to confirm available items: %w",
			itemName,
			err,
		)
	default:
		return camperrors.Wrapf(err, "routing %s to docs/%s", itemName, destination)
	}
}
