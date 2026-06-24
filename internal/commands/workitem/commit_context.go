package workitem

import (
	"context"
	"io"
	"os"

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
