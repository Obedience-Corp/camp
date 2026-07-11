package workitem

import (
	"context"
	"io"
	"os"

	"github.com/Obedience-Corp/camp/internal/git/commit"
	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
	"github.com/Obedience-Corp/camp/internal/workitem/resolver"
)

// CommitContext carries the ambient campaign-tag components resolved from the
// caller's working directory: the active quest, festival, and workitem. Any
// field may be empty when its resolution tier does not match.
type CommitContext struct {
	QuestID     string
	FestivalRef string
	WorkitemRef string
}

// ResolveCommitContext resolves the ambient quest/festival/workitem context
// for a commit tag, mirroring how `camp workitem commit` derives the FE-/WI-/
// qst_ segments but without the staging machinery. It is the shared entry
// point for auto-commit paths (intent and note capture) that want
// `camp commit`-style context inheritance.
//
// Resolution is best-effort: any failure yields a zero-valued field rather
// than an error, so callers always fall back to a bare campaign tag. cwd
// defaults to the process working directory when empty; errw defaults to
// os.Stderr and receives ref-backfill warnings.
func ResolveCommitContext(ctx context.Context, campaignRoot, cwd string, errw io.Writer) CommitContext {
	if errw == nil {
		errw = os.Stderr
	}

	festivalID := inferFestivalIDFromCwd(campaignRoot, cwd)
	res, err := resolver.Resolve(ctx, campaignRoot, resolver.Options{
		Cwd:        cwd,
		FestivalID: festivalID,
	})
	if err != nil || res == nil || res.Workitem == nil {
		return CommitContext{}
	}

	ref, ensureErr := wkitem.EnsureRefForCommit(ctx, campaignRoot, res.Workitem, errw)
	if ensureErr != nil {
		ref = wkitem.RefOf(res.Workitem)
	}

	festivalRef := ""
	if res.Source == resolver.SourceFestival {
		festivalRef = festivalRefFromString(festivalID)
	}

	return CommitContext{
		QuestID:     res.QuestID,
		FestivalRef: festivalRef,
		WorkitemRef: ref,
	}
}

// AmbientCommitOptions resolves the ambient commit context via
// ResolveCommitContext against the process working directory and returns a
// commit.Options seeded with CampaignRoot, CampaignID, and the resolved
// QuestID/FestivalRef/WorkitemRef. It is the shared entry point for every
// intent/note auto-commit call site so they inherit ambient context the
// same way `camp commit` does, instead of each re-implementing the
// resolve-and-populate step. Callers still set Files/PreStaged/
// SelectiveOnly themselves before passing the result into commit.Intent.
//
// errw receives ref-backfill warnings from the underlying resolver; pass
// os.Stderr for CLI callers, or nil to default to it. A TUI or other
// screen-owning caller should pass a writer that does not touch the raw
// terminal (see internal/intent/tui/explorer for the pattern that routes
// this through slog instead).
func AmbientCommitOptions(ctx context.Context, campaignRoot, campaignID string, errw io.Writer) commit.Options {
	cwd, _ := os.Getwd()
	cc := ResolveCommitContext(ctx, campaignRoot, cwd, errw)
	return commit.Options{
		CampaignRoot: campaignRoot,
		CampaignID:   campaignID,
		QuestID:      cc.QuestID,
		FestivalRef:  cc.FestivalRef,
		WorkitemRef:  cc.WorkitemRef,
	}
}
