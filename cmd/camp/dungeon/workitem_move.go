package dungeon

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	navindex "github.com/Obedience-Corp/camp/internal/nav/index"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/ui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

type DungeonMoveCommit struct {
	Config           *config.CampaignConfig
	CampaignRoot     string
	Description      string
	SourcePaths      []string
	DestinationPaths []string
	RewrittenFiles   []string
}

type resolvedWorkitemDungeonTarget struct {
	Item        wkitem.WorkItem
	ItemName    string
	ParentPath  string
	DungeonPath string
	SourcePath  string
}

func moveWorkitemToDungeon(ctx context.Context, cmd *cobra.Command, target, status string) (*DungeonMoveCommit, error) {
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "not in a campaign directory")
	}

	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	items, err := wkitem.Discover(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, camperrors.Wrap(err, "discovering work items")
	}

	item, err := selectWorkitemDungeonTarget(campaignRoot, items, target)
	if err != nil {
		return nil, err
	}

	resolved, err := resolveWorkitemDungeonTarget(campaignRoot, item)
	if err != nil {
		return nil, err
	}

	svc := intdungeon.NewService(campaignRoot, resolved.DungeonPath)
	initResult, err := svc.Init(ctx, intdungeon.InitOptions{})
	if err != nil {
		return nil, camperrors.Wrap(err, "initializing workitem dungeon")
	}

	destinationPaths := append([]string{}, initResult.CreatedFiles...)
	var targetPath string
	if status == "" {
		if err := svc.MoveToDungeon(ctx, resolved.ItemName, resolved.ParentPath); err != nil {
			return nil, WrapDungeonMoveError(err, resolved.ItemName, "dungeon")
		}
		targetPath = filepath.Join(resolved.DungeonPath, resolved.ItemName)
	} else {
		var moveErr error
		targetPath, moveErr = svc.MoveToDungeonStatus(ctx, resolved.ItemName, resolved.ParentPath, status)
		if moveErr != nil {
			return nil, WrapDungeonMoveError(moveErr, resolved.ItemName, status)
		}
	}
	destinationPaths = append(destinationPaths, targetPath)

	recordWorkitemMove(ctx, campaignRoot, resolved.SourcePath, targetPath)
	if ledgerPath, ok := workitemLedgerPathIfExists(campaignRoot); ok {
		destinationPaths = append(destinationPaths, ledgerPath)
	}

	src := RelFromRoot(campaignRoot, resolved.SourcePath)
	dst := RelFromRoot(campaignRoot, targetPath)
	fmt.Printf("%s Moved %s (%s → %s)\n", ui.SuccessIcon(), resolved.ItemName, src, dst)
	invalidateDungeonWorkitemNavigationCache(cmd, campaignRoot)

	description := fmt.Sprintf("Triage workitem %s → %s", resolved.ItemName, dst)
	return &DungeonMoveCommit{
		Config:           cfg,
		CampaignRoot:     campaignRoot,
		Description:      description,
		SourcePaths:      []string{resolved.SourcePath},
		DestinationPaths: destinationPaths,
		RewrittenFiles:   svc.RewrittenLinkFiles(),
	}, nil
}

func selectWorkitemDungeonTarget(campaignRoot string, items []wkitem.WorkItem, target string) (wkitem.WorkItem, error) {
	raw := strings.TrimSpace(target)
	if raw == "" {
		return wkitem.WorkItem{}, camperrors.Newf("workitem target must not be empty")
	}

	matchStages := []struct {
		name  string
		match func(wkitem.WorkItem) bool
	}{
		{
			name: "stable id",
			match: func(item wkitem.WorkItem) bool {
				return item.StableID == raw
			},
		},
		{
			name: "relative path",
			match: func(item wkitem.WorkItem) bool {
				return normalizeWorkitemTargetPath(campaignRoot, raw) == normalizeRelativeWorkitemPath(item.RelativePath)
			},
		},
		{
			name: "slug",
			match: func(item wkitem.WorkItem) bool {
				return filepath.Base(item.RelativePath) == raw
			},
		},
	}

	for _, stage := range matchStages {
		matches := matchingWorkitems(items, stage.match)
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return wkitem.WorkItem{}, camperrors.Newf("workitem target %q is ambiguous by %s; matches: %s", raw, stage.name, formatWorkitemMatches(matches))
		}
	}

	return wkitem.WorkItem{}, camperrors.Newf("workitem %q not found; run 'camp workitem --json' to inspect available workitems", raw)
}

func matchingWorkitems(items []wkitem.WorkItem, match func(wkitem.WorkItem) bool) []wkitem.WorkItem {
	matches := make([]wkitem.WorkItem, 0, 1)
	seen := map[string]bool{}
	for _, item := range items {
		if !match(item) {
			continue
		}
		key := item.Key
		if key == "" {
			key = item.RelativePath
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		matches = append(matches, item)
	}
	return matches
}

func normalizeWorkitemTargetPath(campaignRoot, target string) string {
	cleaned := strings.TrimSpace(target)
	if cleaned == "" {
		return ""
	}
	if filepath.IsAbs(cleaned) {
		if rel, err := filepath.Rel(campaignRoot, cleaned); err == nil {
			cleaned = rel
		}
	}
	return normalizeRelativeWorkitemPath(cleaned)
}

func normalizeRelativeWorkitemPath(path string) string {
	cleaned := filepath.Clean(path)
	if cleaned == "." {
		return ""
	}
	cleaned = strings.TrimPrefix(cleaned, "."+string(filepath.Separator))
	return filepath.ToSlash(cleaned)
}

func resolveWorkitemDungeonTarget(campaignRoot string, item wkitem.WorkItem) (*resolvedWorkitemDungeonTarget, error) {
	if item.ItemKind != wkitem.ItemKindDirectory {
		return nil, camperrors.Newf("workitem %q resolves to %s, but only directory workitems under workflow/<type>/<slug> can be moved with --workitem", item.RelativePath, item.ItemKind)
	}

	rel := normalizeRelativeWorkitemPath(item.RelativePath)
	parts := strings.Split(rel, "/")
	if len(parts) != 3 || parts[0] != "workflow" || parts[1] == "" || parts[2] == "" || parts[1] == "dungeon" || parts[2] == "dungeon" {
		return nil, camperrors.Newf("workitem %q is not under workflow/<type>/<slug>; only directory workitems under workflow/<type>/<slug> can be moved with --workitem", item.RelativePath)
	}

	parentRel := filepath.FromSlash(strings.Join(parts[:2], "/"))
	itemName := parts[2]
	parentPath := filepath.Join(campaignRoot, parentRel)
	dungeonPath := filepath.Join(parentPath, "dungeon")

	return &resolvedWorkitemDungeonTarget{
		Item:        item,
		ItemName:    itemName,
		ParentPath:  parentPath,
		DungeonPath: dungeonPath,
		SourcePath:  filepath.Join(parentPath, itemName),
	}, nil
}

func formatWorkitemMatches(matches []wkitem.WorkItem) string {
	paths := make([]string, 0, len(matches))
	for _, item := range matches {
		paths = append(paths, item.RelativePath)
	}
	sort.Strings(paths)
	return strings.Join(paths, ", ")
}

func invalidateDungeonWorkitemNavigationCache(cmd *cobra.Command, campaignRoot string) {
	if err := navindex.Delete(campaignRoot); err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "%s failed to invalidate navigation cache: %v\n", ui.WarningIcon(), err)
	}
}
