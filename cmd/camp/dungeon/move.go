package dungeon

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var dungeonMoveCmd = &cobra.Command{
	Use:   "move <item> [status]",
	Short: "Move dungeon items between statuses",
	Long: `Move items within the dungeon or from the parent directory into the dungeon.

Without --triage, moves an item already in the dungeon root to a status directory.
With --triage, moves an item from the parent directory into the dungeon.
With --triage and --to-docs, routes an item to an existing campaign-root docs/<subdirectory>.
Moves are always auto-committed so dungeon history remains auditable.

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
	Cmd.AddCommand(dungeonMoveCmd)

	flags := dungeonMoveCmd.Flags()
	flags.Bool("triage", false, "Move from parent directory (not from dungeon root)")
	flags.String("to-docs", "", "Route triage item into an existing campaign-root docs/<subdir> (requires --triage)")
}

func runDungeonMove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	triageMode, _ := cmd.Flags().GetBool("triage")
	toDocs, _ := cmd.Flags().GetString("to-docs")

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
	svc := intdungeon.NewService(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath)

	var description string
	var sourcePaths []string
	var destinationPaths []string

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
			sourcePaths = []string{filepath.Join(cmdCtx.Dungeon.ParentPath, itemName)}
			destinationPaths = []string{targetPath}
		} else if status != "" {
			// Triage directly to a status directory
			targetPath, err := svc.MoveToDungeonStatus(ctx, itemName, cmdCtx.Dungeon.ParentPath, status)
			if err != nil {
				return wrapDungeonMoveError(err, itemName, status)
			}
			src := filepath.Join(relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath), itemName)
			dst := relFromRoot(cmdCtx.CampaignRoot, targetPath)
			fmt.Printf("%s Moved %s (%s → %s)\n", ui.SuccessIcon(), itemName, src, dst)
			description = fmt.Sprintf("Triage %s → dungeon/%s", itemName, status)
			sourcePaths = []string{filepath.Join(cmdCtx.Dungeon.ParentPath, itemName)}
			destinationPaths = []string{targetPath}
		} else {
			// Triage to dungeon root
			if err := svc.MoveToDungeon(ctx, itemName, cmdCtx.Dungeon.ParentPath); err != nil {
				return wrapDungeonMoveError(err, itemName, "dungeon")
			}
			src := filepath.Join(relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath), itemName)
			dst := filepath.Join(relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath), itemName)
			fmt.Printf("%s Moved %s (%s → %s)\n", ui.SuccessIcon(), itemName, src, dst)
			description = fmt.Sprintf("Triage %s → dungeon", itemName)
			sourcePaths = []string{filepath.Join(cmdCtx.Dungeon.ParentPath, itemName)}
			destinationPaths = []string{filepath.Join(cmdCtx.Dungeon.DungeonPath, itemName)}
		}
	} else {
		// Inner move: dungeon root → status directory
		if status == "" {
			return fmt.Errorf("status is required when moving within the dungeon (e.g., completed, archived, someday)")
		}
		targetPath, err := svc.MoveToStatus(ctx, itemName, status)
		if err != nil {
			return wrapDungeonMoveError(err, itemName, status)
		}
		src := filepath.Join(relFromRoot(cmdCtx.CampaignRoot, cmdCtx.Dungeon.DungeonPath), itemName)
		dst := relFromRoot(cmdCtx.CampaignRoot, targetPath)
		fmt.Printf("%s Moved %s (%s → %s)\n", ui.SuccessIcon(), itemName, src, dst)

		relDir, relErr := filepath.Rel(cmdCtx.CampaignRoot, cmdCtx.Dungeon.ParentPath)
		if relErr != nil {
			relDir = cmdCtx.Dungeon.ParentPath
		}
		description = fmt.Sprintf("Moved to dungeon/%s:\n  - %s/%s", status, relDir, itemName)
		sourcePaths = []string{filepath.Join(cmdCtx.Dungeon.DungeonPath, itemName)}
		destinationPaths = []string{targetPath}
	}

	files := commit.NormalizeFiles(cmdCtx.CampaignRoot, destinationPaths...)
	preStaged, err := stageTrackedMoveSourceDeletions(ctx, cmdCtx.CampaignRoot, sourcePaths)
	if err != nil {
		return camperrors.Wrap(err, "staging move source deletions")
	}
	result := commit.Crawl(ctx, commit.CrawlOptions{
		Options: commit.Options{
			CampaignRoot: cmdCtx.CampaignRoot,
			CampaignID:   cmdCtx.Config.ID,
			PreStaged:    preStaged,
		},
		Description: strings.TrimSpace(description),
		Files:       files,
	})
	if result.Committed {
		fmt.Printf("%s %s\n", ui.SuccessIcon(), result.Message)
	} else if result.NoChanges {
		fmt.Printf("%s %s\n", ui.InfoIcon(), result.Message)
	} else if result.Err != nil {
		fmt.Printf("%s Move was applied on disk, but auto-commit failed.\n", ui.WarningIcon())
		fmt.Printf("%s %v\n", ui.WarningIcon(), result.Err)
		return camperrors.Wrap(result.Err, "auto-committing dungeon move")
	} else if result.Message != "" {
		fmt.Printf("%s %s\n", ui.InfoIcon(), result.Message)
	}

	return nil
}

func stageTrackedMoveSourceDeletions(ctx context.Context, campaignRoot string, sourcePaths []string) ([]string, error) {
	sources := commit.NormalizeFiles(campaignRoot, sourcePaths...)
	if len(sources) == 0 {
		return nil, nil
	}
	tracked, err := git.FilterTracked(ctx, campaignRoot, sources)
	if err != nil {
		return nil, err
	}
	if len(tracked) == 0 {
		return nil, nil
	}
	if err := git.StageTrackedChanges(ctx, campaignRoot, tracked...); err != nil {
		return nil, err
	}
	return tracked, nil
}

func wrapDungeonMoveError(err error, itemName, status string) error {
	switch {
	case errors.Is(err, intdungeon.ErrAlreadyExists):
		return fmt.Errorf(
			"cannot move %q to %q because destination already exists; choose another status or rename the item: %w",
			itemName,
			status,
			err,
		)
	case errors.Is(err, intdungeon.ErrInvalidStatus):
		return fmt.Errorf(
			"invalid status %q for %q; use a single directory name like completed, archived, or someday: %w",
			status,
			itemName,
			err,
		)
	case errors.Is(err, intdungeon.ErrInvalidItemPath):
		return fmt.Errorf(
			"invalid item path %q; use a direct child file or directory name from the current dungeon context (no slashes or traversal). Run 'camp dungeon list --triage' or 'camp dungeon list' to confirm available items: %w",
			itemName,
			err,
		)
	case errors.Is(err, intdungeon.ErrNotFound):
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
	case errors.Is(err, intdungeon.ErrAlreadyExists):
		return fmt.Errorf(
			"cannot route %q to docs/%s because destination already exists; choose another docs destination or rename the item: %w",
			itemName,
			destination,
			err,
		)
	case errors.Is(err, intdungeon.ErrInvalidDocsDestination):
		return fmt.Errorf(
			"invalid docs destination %q; use an existing subdirectory under campaign-root docs/ (for example: --to-docs architecture/api): %w",
			destination,
			err,
		)
	case errors.Is(err, intdungeon.ErrInvalidItemPath):
		return fmt.Errorf(
			"invalid item path %q; use a direct child file or directory name from the resolved triage context (no slashes or traversal). Run 'camp dungeon list --triage' to confirm available items: %w",
			itemName,
			err,
		)
	case errors.Is(err, intdungeon.ErrNotFound):
		return fmt.Errorf(
			"item %q was not found in the resolved triage context; run 'camp dungeon list --triage' to confirm available items: %w",
			itemName,
			err,
		)
	default:
		return camperrors.Wrapf(err, "routing %s to docs/%s", itemName, destination)
	}
}
