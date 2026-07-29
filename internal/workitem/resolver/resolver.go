// Package resolver implements the deterministic workitem-context resolution
// pipeline for "which workitem should this commit/operation count toward?" It
// is imported by commit wrappers and the `camp workitem resolve` and
// `camp workitem doctor` commands.
package resolver

import (
	"context"
	"errors"
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
	// DisableCurrent prevents the per-machine current.yaml fallback. Generic
	// commit wrappers use this to avoid silently attributing unrelated changes
	// to a stale session-wide workitem selection.
	DisableCurrent bool
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

	var err error
	root, err = canonicalPath(root)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve campaign root")
	}

	cwd := opts.Cwd
	if cwd == "" {
		c, err := os.Getwd()
		if err != nil {
			return nil, camperrors.Wrap(err, "get cwd")
		}
		cwd = c
	}
	cwd, err = canonicalPath(cwd)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve cwd")
	}

	result := &Resolution{Source: SourceNone, Reason: "no tier matched"}
	currentTier := func() (*workitem.WorkItem, TraceStep, error) {
		if opts.DisableCurrent {
			return nil, TraceStep{Tier: SourceCurrent, Result: "skip", Detail: "current.yaml disabled by caller"}, nil
		}
		return resolveCurrent(ctx, root)
	}
	tiers := []resolveTier{
		{SourceExplicit, func() (*workitem.WorkItem, TraceStep, error) {
			return resolveExplicit(ctx, root, opts)
		}, "explicit --workitem flag"},
		{SourceAncestor, func() (*workitem.WorkItem, TraceStep, error) {
			return resolveAncestor(ctx, root, cwd)
		}, "nearest ancestor .workitem"},
		{SourceLink, func() (*workitem.WorkItem, TraceStep, error) {
			return resolveLink(ctx, root, cwd)
		}, ""},
		{SourceFestival, func() (*workitem.WorkItem, TraceStep, error) {
			return resolveFestival(ctx, root, opts.FestivalID)
		}, ""},
		{SourceCurrent, currentTier, ""},
	}
	for _, tier := range tiers {
		wi, step, err := tier.fn()
		result.Trace = append(result.Trace, step)
		if err != nil {
			// Operational failures (cancellation, registry parse, unexpected
			// I/O, malformed selector input) bubble up so callers don't get a
			// silently-misattributed lower-priority workitem. The only
			// recoverable case is the stale-link condition where a primary
			// path link points to a workitem that no longer exists; tier
			// helpers signal that by encoding the failure in step.Result
			// without returning a non-nil err.
			result.Source = tier.source
			result.Reason = firstNonEmpty(step.Detail, err.Error())
			return result, err
		}
		if wi != nil {
			result.Workitem = wi
			result.Source = tier.source
			result.Reason = firstNonEmpty(tier.reason, step.Detail)
			result.QuestID = questIDOf(wi)
			return result, nil
		}
	}

	result.Trace = append(result.Trace, TraceStep{Tier: SourceNone, Result: "match", Detail: "no workitem context"})
	return result, nil
}

type resolveTier struct {
	source Source
	fn     func() (*workitem.WorkItem, TraceStep, error)
	reason string
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	return abs, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func resolveExplicit(ctx context.Context, root string, opts Options) (*workitem.WorkItem, TraceStep, error) {
	if opts.Explicit == "" {
		return nil, TraceStep{Tier: SourceExplicit, Result: "skip", Detail: "no --workitem flag"}, nil
	}
	wi, err := selector.Resolve(ctx, root, opts.Explicit, selector.ResolveOptions{AllowFuzzy: opts.AllowFuzzy})
	if err != nil {
		// User asked for an explicit workitem; if we cannot resolve it,
		// fail loudly rather than fall through to a lower-priority tier
		// and silently tag against the wrong context.
		return nil, TraceStep{Tier: SourceExplicit, Result: "error", Detail: err.Error()},
			camperrors.Wrap(err, "resolve --workitem "+opts.Explicit)
	}
	return wi, TraceStep{Tier: SourceExplicit, Result: "match", Detail: wi.Key}, nil
}

func resolveAncestor(ctx context.Context, root, cwd string) (*workitem.WorkItem, TraceStep, error) {
	if cwd == "" || root == "" {
		return nil, TraceStep{Tier: SourceAncestor, Result: "skip", Detail: "no cwd"}, nil
	}
	dir := cwd
	for {
		markerPath := filepath.Join(dir, workitem.MetadataFilename)
		if info, err := os.Stat(markerPath); err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(root, dir)
			wi, err := selector.Resolve(ctx, root, filepath.ToSlash(rel), selector.ResolveOptions{})
			if err == nil {
				return wi, TraceStep{Tier: SourceAncestor, Result: "match", Detail: filepath.ToSlash(rel)}, nil
			}
			// We found a .workitem on disk but the selector cannot
			// parse it. Fail loudly rather than skip; a malformed
			// marker that gets silently bypassed is exactly the
			// "wrong-context tag" case the reviewer flagged.
			if !errors.Is(err, selector.ErrSelectorNotFound) {
				return nil, TraceStep{Tier: SourceAncestor, Result: "error", Detail: err.Error()},
					camperrors.Wrap(err, "resolve ancestor "+filepath.ToSlash(rel))
			}
		}
		if dir == root || dir == filepath.Dir(dir) {
			break
		}
		dir = filepath.Dir(dir)
		if pathOutside(root, dir) {
			break
		}
	}
	return nil, TraceStep{Tier: SourceAncestor, Result: "miss", Detail: "no .workitem found between cwd and campaign root"}, nil
}

func resolveLink(ctx context.Context, root, cwd string) (*workitem.WorkItem, TraceStep, error) {
	registry, err := links.Load(ctx, root)
	if err != nil {
		return nil, TraceStep{Tier: SourceLink, Result: "error", Detail: err.Error()},
			camperrors.Wrap(err, "load links registry")
	}
	rel, err := filepath.Rel(root, cwd)
	if err != nil || relOutsideRoot(rel) {
		return nil, TraceStep{Tier: SourceLink, Result: "skip", Detail: "cwd outside campaign root"}, nil
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
		return nil, TraceStep{Tier: SourceLink, Result: "miss", Detail: "no primary path link covers cwd"}, nil
	}
	wi, err := selector.Resolve(ctx, root, best.WorkitemID, selector.ResolveOptions{})
	if err != nil {
		// Stale-link: the registry references a workitem that no longer
		// exists on disk. Record the diagnostic and let the orchestrator
		// fall through to the next tier. Any other selector failure is
		// returned so the caller sees the operational problem.
		if errors.Is(err, selector.ErrSelectorNotFound) {
			return nil, TraceStep{
				Tier:   SourceLink,
				Result: "error",
				Detail: "primary link " + best.ID + " points to missing workitem " + best.WorkitemID,
			}, nil
		}
		detail := "primary link " + best.ID + " could not resolve workitem " + best.WorkitemID + ": " + err.Error()
		return nil, TraceStep{Tier: SourceLink, Result: "error", Detail: detail},
			camperrors.NewValidation("workitem_link", detail, err)
	}
	return wi, TraceStep{
		Tier:   SourceLink,
		Result: "match",
		Detail: "via link " + best.ID + " on " + string(best.Scope.Kind) + ":" + best.Scope.Path,
	}, nil
}

func resolveFestival(ctx context.Context, root, festivalID string) (*workitem.WorkItem, TraceStep, error) {
	if festivalID == "" {
		return nil, TraceStep{Tier: SourceFestival, Result: "skip", Detail: "no festival id"}, nil
	}
	registry, err := links.Load(ctx, root)
	if err != nil {
		return nil, TraceStep{Tier: SourceFestival, Result: "error", Detail: err.Error()},
			camperrors.Wrap(err, "load links registry")
	}
	for i := range registry.Links {
		link := &registry.Links[i]
		if link.Role != links.RolePrimary || link.Scope.Kind != links.ScopeFestival {
			continue
		}
		if !FestivalScopeMatches(link.Scope.Path, festivalID) {
			continue
		}
		wi, err := selector.Resolve(ctx, root, link.WorkitemID, selector.ResolveOptions{})
		if err == nil {
			return wi, TraceStep{
				Tier:   SourceFestival,
				Result: "match",
				Detail: "via link " + link.ID + " on festival " + link.Scope.Path,
			}, nil
		}
		// Stale-link parallel of the path-link case: continue looking for
		// other festival links if this one points to a missing workitem.
		// Anything other than ErrSelectorNotFound is operational and bubbles up.
		if !errors.Is(err, selector.ErrSelectorNotFound) {
			detail := "festival link " + link.ID + " could not resolve workitem " + link.WorkitemID + ": " + err.Error()
			return nil, TraceStep{Tier: SourceFestival, Result: "error", Detail: detail},
				camperrors.NewValidation("festival_link", detail, err)
		}
	}
	return nil, TraceStep{Tier: SourceFestival, Result: "miss", Detail: "no festival link matches " + festivalID}, nil
}

func resolveCurrent(ctx context.Context, root string) (*workitem.WorkItem, TraceStep, error) {
	cur, err := links.LoadCurrent(ctx, root)
	if err != nil {
		return nil, TraceStep{Tier: SourceCurrent, Result: "error", Detail: err.Error()},
			camperrors.Wrap(err, "load current.yaml")
	}
	if cur == nil {
		return nil, TraceStep{Tier: SourceCurrent, Result: "skip", Detail: "no current.yaml"}, nil
	}
	wi, err := selector.Resolve(ctx, root, cur.WorkitemID, selector.ResolveOptions{})
	if err != nil {
		// Stale current selection: file points to a workitem that no
		// longer exists. Record the diagnostic and let the orchestrator
		// fall through; current is the lowest-priority tier so any
		// match below it (none in practice today) would be acceptable.
		// Any other failure is operational.
		if errors.Is(err, selector.ErrSelectorNotFound) {
			return nil, TraceStep{
				Tier:   SourceCurrent,
				Result: "error",
				Detail: "current.yaml points to missing workitem " + cur.WorkitemID,
			}, nil
		}
		detail := "current.yaml could not resolve workitem " + cur.WorkitemID + ": " + err.Error()
		return nil, TraceStep{Tier: SourceCurrent, Result: "error", Detail: detail},
			camperrors.NewValidation("current_workitem", detail, err)
	}
	return wi, TraceStep{Tier: SourceCurrent, Result: "match", Detail: "via current.yaml"}, nil
}

func pathMatchesPrefix(cwdRel, scopePath string) bool {
	if cwdRel == scopePath {
		return true
	}
	return strings.HasPrefix(cwdRel, scopePath+"/")
}

func pathOutside(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err != nil || relOutsideRoot(rel)
}

func relOutsideRoot(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

// FestivalScopeMatches reports whether a festival link scope path corresponds
// to the given festival id. It is the shared rule used both when resolving a
// workitem from festival context and when selecting which festival's files to
// stage, so the commit tag and the staged paths cannot disagree.
func FestivalScopeMatches(scopePath, festivalID string) bool {
	if scopePath == festivalID {
		return true
	}
	base := filepath.Base(scopePath)
	return base == festivalID || strings.HasSuffix(base, "-"+festivalID)
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
