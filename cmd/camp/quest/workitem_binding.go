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
	questsvc "github.com/Obedience-Corp/camp/internal/quest"
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

// resolveWorkitemEnrichment discovers workitem metadata (Title, Summary) for a
// campaign-relative path. It returns zero values when the path is not a
// discoverable workitem — enrichment is best-effort and never blocks linking.
func resolveWorkitemEnrichment(ctx context.Context, root, relPath string) (title, summary string) {
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return "", ""
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	items, err := wkitem.Discover(ctx, root, resolver)
	if err != nil {
		return "", ""
	}
	normalized := filepath.ToSlash(filepath.Clean(relPath))
	for _, item := range items {
		if filepath.ToSlash(filepath.Clean(item.RelativePath)) == normalized {
			return item.Title, item.Summary
		}
	}
	return "", ""
}

// enrichFromLinkedWorkitem fills empty quest Purpose/Description from the
// workitem at relPath, if that path resolves to a discoverable workitem. The
// enrichment is best-effort: discovery failures or non-workitem paths are
// silent no-ops. User-supplied fields are never overwritten.
//
// When enrichment writes nothing, the prior MutationResult is returned
// unchanged so autoCommitQuest still stages the post-Link quest file. When it
// does write, Files is the union of prior and enriched paths.
func enrichFromLinkedWorkitem(ctx context.Context, qctx *questCommandContext, result *questsvc.MutationResult, relPath string) (*questsvc.MutationResult, error) {
	if result == nil || result.Quest == nil {
		return result, nil
	}
	title, summary := resolveWorkitemEnrichment(ctx, qctx.campaignRoot, relPath)
	if title == "" && summary == "" {
		return result, nil
	}
	enriched, err := qctx.service.EnrichFromWorkitem(ctx, result.Quest.ID, questsvc.WorkitemEnrichment{
		Title:   title,
		Summary: summary,
	})
	if err != nil || enriched == nil || len(enriched.Files) == 0 {
		// Enrichment failure or no-op must not discard the post-Link Files.
		return result, nil
	}
	enriched.Files = unionFiles(result.Files, enriched.Files)
	return enriched, nil
}

func unionFiles(prior, extra []string) []string {
	seen := make(map[string]struct{}, len(prior)+len(extra))
	out := make([]string, 0, len(prior)+len(extra))
	for _, p := range append(append([]string{}, prior...), extra...) {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
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
