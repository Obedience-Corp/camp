// Package resolver implements the deterministic workitem-context resolution
// pipeline documented in `internal/workitem/links/SCHEMA.md` §7. It is the
// single source of truth for "which workitem should this commit/operation
// count toward?" and is imported by commit wrappers in sequence 03 and the
// `camp workitem resolve` and `camp workitem doctor` commands.
package resolver

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/links"
	"github.com/Obedience-Corp/camp/internal/workitem/selector"
)

// Source identifies which resolution tier produced a result.
type Source string

const (
	SourceExplicit Source = "explicit"
	SourceAncestor Source = "ancestor"
	SourceLink     Source = "link"
	SourceFestival Source = "festival"
	SourceCurrent  Source = "current"
	SourceNone     Source = "none"
)

// Options inputs to Resolve. Cwd defaults to os.Getwd() when empty.
type Options struct {
	Explicit   string
	Cwd        string
	FestivalID string
	AllowFuzzy bool
}

// TraceStep records what one tier of the resolver did. Surfaced via --json
// and --explain so debugging "why did this resolve to X?" is mechanical.
type TraceStep struct {
	Tier   Source `json:"tier"`
	Result string `json:"result"`           // "match" | "miss" | "skip" | "error"
	Detail string `json:"detail,omitempty"` // human-readable note
}

// Resolution is what Resolve returns.
type Resolution struct {
	Workitem *workitem.WorkItem `json:"workitem,omitempty"`
	Source   Source             `json:"source"`
	Reason   string             `json:"reason"`
	QuestID  string             `json:"quest_id,omitempty"`
	Trace    []TraceStep        `json:"trace"`
}

// Resolve runs the six-tier pipeline and returns the first match (or a
// Resolution with Source=none when no tier matches).
func Resolve(ctx context.Context, root string, opts Options) (*Resolution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cwd := opts.Cwd
	if cwd == "" {
		c, err := os.Getwd()
		if err != nil {
			return nil, camperrors.Wrap(err, "get cwd")
		}
		cwd = c
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}

	result := &Resolution{Source: SourceNone, Reason: "no tier matched"}

	if wi, step := resolveExplicit(ctx, root, opts); wi != nil {
		result.Workitem = wi
		result.Source = SourceExplicit
		result.Reason = "explicit --workitem flag"
		result.QuestID = questIDOf(wi)
		result.Trace = append(result.Trace, step)
		return result, nil
	} else {
		result.Trace = append(result.Trace, step)
	}

	if wi, step := resolveAncestor(ctx, root, cwd); wi != nil {
		result.Workitem = wi
		result.Source = SourceAncestor
		result.Reason = "nearest ancestor .workitem"
		result.QuestID = questIDOf(wi)
		result.Trace = append(result.Trace, step)
		return result, nil
	} else {
		result.Trace = append(result.Trace, step)
	}

	if wi, step := resolveLink(ctx, root, cwd); wi != nil {
		result.Workitem = wi
		result.Source = SourceLink
		result.Reason = step.Detail
		result.QuestID = questIDOf(wi)
		result.Trace = append(result.Trace, step)
		return result, nil
	} else {
		result.Trace = append(result.Trace, step)
	}

	if wi, step := resolveFestival(ctx, root, opts.FestivalID); wi != nil {
		result.Workitem = wi
		result.Source = SourceFestival
		result.Reason = step.Detail
		result.QuestID = questIDOf(wi)
		result.Trace = append(result.Trace, step)
		return result, nil
	} else {
		result.Trace = append(result.Trace, step)
	}

	if wi, step := resolveCurrent(ctx, root); wi != nil {
		result.Workitem = wi
		result.Source = SourceCurrent
		result.Reason = step.Detail
		result.QuestID = questIDOf(wi)
		result.Trace = append(result.Trace, step)
		return result, nil
	} else {
		result.Trace = append(result.Trace, step)
	}

	result.Trace = append(result.Trace, TraceStep{Tier: SourceNone, Result: "match", Detail: "no workitem context"})
	return result, nil
}

func resolveExplicit(ctx context.Context, root string, opts Options) (*workitem.WorkItem, TraceStep) {
	if opts.Explicit == "" {
		return nil, TraceStep{Tier: SourceExplicit, Result: "skip", Detail: "no --workitem flag"}
	}
	wi, err := selector.Resolve(ctx, root, opts.Explicit, selector.ResolveOptions{AllowFuzzy: opts.AllowFuzzy})
	if err != nil {
		return nil, TraceStep{Tier: SourceExplicit, Result: "error", Detail: err.Error()}
	}
	return wi, TraceStep{Tier: SourceExplicit, Result: "match", Detail: wi.Key}
}

func resolveAncestor(ctx context.Context, root, cwd string) (*workitem.WorkItem, TraceStep) {
	if cwd == "" || root == "" {
		return nil, TraceStep{Tier: SourceAncestor, Result: "skip", Detail: "no cwd"}
	}
	dir := cwd
	for {
		markerPath := filepath.Join(dir, workitem.MetadataFilename)
		if info, err := os.Stat(markerPath); err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(root, dir)
			wi, err := selector.Resolve(ctx, root, filepath.ToSlash(rel), selector.ResolveOptions{})
			if err == nil {
				return wi, TraceStep{Tier: SourceAncestor, Result: "match", Detail: filepath.ToSlash(rel)}
			}
		}
		if dir == root || dir == filepath.Dir(dir) {
			break
		}
		dir = filepath.Dir(dir)
		// Guard: don't walk above the campaign root.
		if !strings.HasPrefix(dir, root) {
			break
		}
	}
	return nil, TraceStep{Tier: SourceAncestor, Result: "miss", Detail: "no .workitem found between cwd and campaign root"}
}

func resolveLink(ctx context.Context, root, cwd string) (*workitem.WorkItem, TraceStep) {
	registry, err := links.Load(ctx, root)
	if err != nil {
		return nil, TraceStep{Tier: SourceLink, Result: "error", Detail: err.Error()}
	}
	rel, err := filepath.Rel(root, cwd)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, TraceStep{Tier: SourceLink, Result: "skip", Detail: "cwd outside campaign root"}
	}
	relSlash := filepath.ToSlash(rel)

	var best *links.Link
	bestLen := -1
	for i := range registry.Links {
		link := &registry.Links[i]
		if link.Role != links.RolePrimary {
			continue
		}
		switch link.Scope.Kind {
		case links.ScopeProject, links.ScopeRepo, links.ScopeCampaignPath, links.ScopeWorktree:
		default:
			continue
		}
		scopePath := strings.TrimRight(link.Scope.Path, "/")
		if !pathMatchesPrefix(relSlash, scopePath) {
			continue
		}
		if len(scopePath) > bestLen {
			best = link
			bestLen = len(scopePath)
		}
	}
	if best == nil {
		return nil, TraceStep{Tier: SourceLink, Result: "miss", Detail: "no primary path link covers cwd"}
	}
	wi, err := selector.Resolve(ctx, root, best.WorkitemID, selector.ResolveOptions{})
	if err != nil {
		// Workitem on disk vanished; surface as a tier miss with a note —
		// doctor will report the same condition as broken-link.
		return nil, TraceStep{
			Tier:   SourceLink,
			Result: "error",
			Detail: "primary link " + best.ID + " points to missing workitem " + best.WorkitemID,
		}
	}
	return wi, TraceStep{
		Tier:   SourceLink,
		Result: "match",
		Detail: "via link " + best.ID + " on " + string(best.Scope.Kind) + ":" + best.Scope.Path,
	}
}

func resolveFestival(ctx context.Context, root, festivalID string) (*workitem.WorkItem, TraceStep) {
	if festivalID == "" {
		return nil, TraceStep{Tier: SourceFestival, Result: "skip", Detail: "no festival id"}
	}
	registry, err := links.Load(ctx, root)
	if err != nil {
		return nil, TraceStep{Tier: SourceFestival, Result: "error", Detail: err.Error()}
	}
	for i := range registry.Links {
		link := &registry.Links[i]
		if link.Role != links.RolePrimary || link.Scope.Kind != links.ScopeFestival {
			continue
		}
		if !festivalScopeMatches(link.Scope.Path, festivalID) {
			continue
		}
		wi, err := selector.Resolve(ctx, root, link.WorkitemID, selector.ResolveOptions{})
		if err == nil {
			return wi, TraceStep{
				Tier:   SourceFestival,
				Result: "match",
				Detail: "via link " + link.ID + " on festival " + link.Scope.Path,
			}
		}
	}
	return nil, TraceStep{Tier: SourceFestival, Result: "miss", Detail: "no festival link matches " + festivalID}
}

func resolveCurrent(ctx context.Context, root string) (*workitem.WorkItem, TraceStep) {
	cur, err := links.LoadCurrent(ctx, root)
	if err != nil {
		return nil, TraceStep{Tier: SourceCurrent, Result: "error", Detail: err.Error()}
	}
	if cur == nil {
		return nil, TraceStep{Tier: SourceCurrent, Result: "skip", Detail: "no current.yaml"}
	}
	wi, err := selector.Resolve(ctx, root, cur.WorkitemID, selector.ResolveOptions{})
	if err != nil {
		return nil, TraceStep{Tier: SourceCurrent, Result: "error", Detail: err.Error()}
	}
	return wi, TraceStep{Tier: SourceCurrent, Result: "match", Detail: "via current.yaml"}
}

func pathMatchesPrefix(cwdRel, scopePath string) bool {
	if cwdRel == scopePath {
		return true
	}
	return strings.HasPrefix(cwdRel, scopePath+"/")
}

func festivalScopeMatches(scopePath, festivalID string) bool {
	if scopePath == festivalID {
		return true
	}
	base := filepath.Base(scopePath)
	return base == festivalID
}

// questIDOf returns the workitem's quest id, or "" if the field is not set.
// The QuestID field is added in sequence 03 task 01; this helper hides the
// access so sequences 02 callers do not break when the field is empty.
func questIDOf(wi *workitem.WorkItem) string {
	if wi == nil {
		return ""
	}
	// QuestID lives on Metadata, not WorkItem. WorkItem.SourceMetadata may
	// carry it as a hint surfaced by the discoverer; check there first.
	// Until sequence 03 plumbs it through, this stays a no-op.
	if wi.SourceMetadata == nil {
		return ""
	}
	if v, ok := wi.SourceMetadata["quest_id"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

