//go:build dev

package quest

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	questtui "github.com/Obedience-Corp/camp/internal/quest/tui"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
)

// resolveWorkitemPath resolves a --workitem selector to a campaign-relative
// path using the same resolver family the workitem commands use (ref, stable
// id, key, path, directory slug, festival id). It is called before any quest is
// created so a bad selector fails fast without leaving an orphan quest.
func resolveWorkitemPath(ctx context.Context, sel string) (string, error) {
	_, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return "", camperrors.Wrap(err, "not in a campaign directory")
	}
	root, err = pathutil.ResolveRoot(root)
	if err != nil {
		return "", camperrors.Wrap(err, "resolving campaign root")
	}
	item, err := selector.Resolve(ctx, root, sel, selector.ResolveOptions{})
	if err != nil {
		return "", err
	}
	return item.RelativePath, nil
}

// gatherWorkitemChoices enumerates the workitems offered by the interactive
// binding picker: the same active, non-dungeon set that `camp workitem` serves
// (discovery never scans dungeon/completed directories), including festivals.
func gatherWorkitemChoices(ctx context.Context) ([]questtui.WorkitemChoice, error) {
	cfg, root, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, camperrors.Wrap(err, "not in a campaign directory")
	}
	root, err = pathutil.ResolveRoot(root)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolving campaign root")
	}

	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return nil, camperrors.Wrap(err, "discovering workitems")
	}
	wkitem.Sort(items)

	choices := make([]questtui.WorkitemChoice, 0, len(items))
	for _, item := range items {
		choices = append(choices, questtui.WorkitemChoice{
			Path:  item.RelativePath,
			Title: workitemChoiceTitle(item),
			Ref:   workitemChoiceRef(item),
			Type:  string(item.WorkflowType),
		})
	}
	return choices, nil
}

func workitemChoiceTitle(item wkitem.WorkItem) string {
	if item.Title != "" {
		return item.Title
	}
	return filepath.Base(item.RelativePath)
}

func workitemChoiceRef(item wkitem.WorkItem) string {
	if ref, ok := item.SourceMetadata["ref"].(string); ok && ref != "" {
		return ref
	}
	return item.SourceID
}

// completeWorkitemSelector offers workitem refs, stable ids, directory slugs,
// and festival ids as shell completions for the --workitem flag.
func completeWorkitemSelector(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	choices, err := gatherWorkitemChoices(cmd.Context())
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	prefix := strings.ToLower(toComplete)
	seen := map[string]struct{}{}
	var matches []string
	for _, choice := range choices {
		for _, candidate := range []string{choice.Ref, filepath.Base(choice.Path)} {
			if candidate == "" || !strings.HasPrefix(strings.ToLower(candidate), prefix) {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			matches = append(matches, candidate)
		}
	}
	return matches, cobra.ShellCompDirectiveNoFileComp
}
