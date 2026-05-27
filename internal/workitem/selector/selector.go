// Package selector resolves user-supplied workitem selector strings to a
// concrete WorkItem. The resolution order is explicit and stable so CLI
// ergonomics stay predictable.
package selector

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/paths"
	"github.com/Obedience-Corp/camp/internal/workitem"
)

// ResolveOptions tunes selector resolution.
type ResolveOptions struct {
	// AllowFuzzy enables title/key substring matching when no exact match is
	// found. Reserved for interactive callers; commit-path resolvers should
	// leave this false.
	AllowFuzzy bool
}

var (
	ErrSelectorAmbiguous = camperrors.NewValidation("selector", "ambiguous", nil)
	ErrSelectorNotFound  = camperrors.NewValidation("selector", "not found", nil)
)

// Resolve looks up a workitem by query. It returns:
//   - the matched workitem when exactly one workitem matches the highest
//     priority that produced any match;
//   - a wrapped ErrSelectorAmbiguous when multiple workitems tie at that priority;
//   - a wrapped ErrSelectorNotFound when nothing matches.
//
// Resolution order:
//  1. exact stable .workitem id
//  2. exact key (`<type>:<path>`)
//  3. exact RelativePath
//  4. exact directory-base slug
//  5. fuzzy substring on key or title (only when opts.AllowFuzzy)
func Resolve(ctx context.Context, root string, query string, opts ResolveOptions) (*workitem.WorkItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, camperrors.NewValidation("selector", "selector must not be empty", nil)
	}

	items, err := discover(ctx, root)
	if err != nil {
		return nil, err
	}

	type matcher struct {
		name string
		eq   func(workitem.WorkItem) bool
	}
	matchers := []matcher{
		{"id", func(w workitem.WorkItem) bool { return w.StableID != "" && w.StableID == query }},
		{"key", func(w workitem.WorkItem) bool { return strings.EqualFold(w.Key, query) }},
		{"path", func(w workitem.WorkItem) bool { return w.RelativePath == strings.TrimRight(query, "/") }},
		{"slug", func(w workitem.WorkItem) bool { return strings.EqualFold(filepath.Base(w.RelativePath), query) }},
	}
	for _, m := range matchers {
		var matched []workitem.WorkItem
		for _, item := range items {
			if m.eq(item) {
				matched = append(matched, item)
			}
		}
		if len(matched) == 1 {
			return &matched[0], nil
		}
		if len(matched) > 1 {
			return nil, ambiguous(query, m.name, matched)
		}
	}

	if opts.AllowFuzzy {
		lower := strings.ToLower(query)
		var matched []workitem.WorkItem
		for _, item := range items {
			if strings.Contains(strings.ToLower(item.Key), lower) ||
				strings.Contains(strings.ToLower(item.Title), lower) {
				matched = append(matched, item)
			}
		}
		if len(matched) == 1 {
			return &matched[0], nil
		}
		if len(matched) > 1 {
			return nil, ambiguous(query, "fuzzy", matched)
		}
	}

	return nil, notFound(query, items)
}

func discover(ctx context.Context, root string) ([]workitem.WorkItem, error) {
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return nil, camperrors.Wrap(err, "load campaign config")
	}
	resolver := paths.NewResolverFromConfig(root, cfg)
	return workitem.Discover(ctx, root, resolver)
}

func ambiguous(query, stage string, matches []workitem.WorkItem) error {
	keys := make([]string, 0, len(matches))
	for _, m := range matches {
		keys = append(keys, m.Key)
	}
	sort.Strings(keys)
	return camperrors.Wrap(ErrSelectorAmbiguous,
		"selector "+query+" matched "+stage+" against multiple workitems: "+strings.Join(keys, ", "))
}

func notFound(query string, items []workitem.WorkItem) error {
	suggestions := nearMatches(query, items, 3)
	msg := "no workitem matched selector " + query
	if len(suggestions) > 0 {
		msg += "; did you mean: " + strings.Join(suggestions, ", ")
	}
	return camperrors.Wrap(ErrSelectorNotFound, msg)
}

func nearMatches(query string, items []workitem.WorkItem, limit int) []string {
	lower := strings.ToLower(query)
	scored := make([][2]string, 0, len(items))
	for _, item := range items {
		base := strings.ToLower(filepath.Base(item.RelativePath))
		if strings.Contains(base, lower) {
			scored = append(scored, [2]string{base, item.Key})
		}
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i][0] < scored[j][0] })
	out := make([]string, 0, limit)
	for i := 0; i < len(scored) && i < limit; i++ {
		out = append(out, scored[i][1])
	}
	return out
}
