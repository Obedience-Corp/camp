package index

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav"
)

// RunShortcutResolution contains the working directory and remaining command
// arguments for a camp run @shortcut invocation.
type RunShortcutResolution struct {
	WorkDir               string
	CommandArgs           []string
	BypassProjectDispatch bool
}

// ResolveRunShortcut resolves a navigation shortcut for camp run.
func ResolveRunShortcut(ctx context.Context, root string, cfg *config.CampaignConfig, shortcutName string, remainingArgs []string) (*RunShortcutResolution, error) {
	sc, ok := cfg.Shortcuts()[shortcutName]
	if !ok {
		return nil, camperrors.Wrapf(camperrors.ErrNotFound, "unknown shortcut %q (run 'camp shortcuts' to see available shortcuts)", shortcutName)
	}

	if !sc.IsNavigation() {
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "shortcut %q is not a navigation shortcut (only shortcuts with paths can be used)", shortcutName)
	}

	if nav.IsStandardPath(sc.Path) && len(remainingArgs) > 0 {
		if resolved, ok, err := resolveRunStandardShortcut(ctx, root, cfg, shortcutName, remainingArgs); ok || err != nil {
			return resolved, err
		}
	}

	workDir, err := nav.ResolveRelativePathNavigation(ctx, root, sc.Path, "")
	if err != nil {
		return nil, camperrors.Wrapf(camperrors.ErrNotFound, "shortcut directory does not exist: %s", filepath.Join(root, sc.Path))
	}

	return &RunShortcutResolution{
		WorkDir:     workDir,
		CommandArgs: remainingArgs,
	}, nil
}

func resolveRunStandardShortcut(ctx context.Context, root string, cfg *config.CampaignConfig, shortcutName string, remainingArgs []string) (*RunShortcutResolution, bool, error) {
	configMappings := nav.BuildCategoryMappings(cfg.Shortcuts())
	syntheticArgs := append([]string{shortcutName}, remainingArgs...)
	parseResult := nav.ParseShortcut(syntheticArgs, configMappings)
	if parseResult.Query == "" {
		return nil, false, nil
	}

	queryParts := strings.Fields(parseResult.Query)
	projectQuery := queryParts[0]
	consumed := 1 // project

	resolveResult, err := Resolve(ctx, ResolveOptions{
		CampaignRoot: root,
		Category:     parseResult.Category,
		Query:        projectQuery,
	})
	if err != nil {
		return nil, true, err
	}

	workDir := resolveResult.Path
	if len(queryParts) > 1 && resolveResult.Target != nil && resolveResult.Target.HasShortcut(queryParts[1]) {
		workDir = resolveResult.Target.JumpPath(queryParts[1])
		consumed = 2 // project + subshortcut
	}

	if consumed >= len(remainingArgs) {
		return nil, true, camperrors.Wrap(camperrors.ErrInvalidInput, "no command specified")
	}
	commandArgs := remainingArgs[consumed:]

	if stat, err := os.Stat(workDir); err != nil || !stat.IsDir() {
		return nil, true, camperrors.Wrapf(camperrors.ErrNotFound, "directory does not exist: %s", workDir)
	}

	return &RunShortcutResolution{
		WorkDir:               workDir,
		CommandArgs:           commandArgs,
		BypassProjectDispatch: true,
	}, true, nil
}
