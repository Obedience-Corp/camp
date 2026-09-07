package selector

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/Obedience-Corp/camp/internal/workitem"
)

// EnsureCommitRef returns the WI- ref of the workitem named by query,
// backfilling one when it has none. Returns "" when the query does not resolve
// or the workitem carries no ref by design, such as a festival. Callers must
// invoke it before a move: the backfill writes to a file the move relocates.
func EnsureCommitRef(ctx context.Context, campaignRoot, query string, errw io.Writer) string {
	if strings.TrimSpace(query) == "" {
		return ""
	}
	if errw == nil {
		errw = os.Stderr
	}
	wi, err := Resolve(ctx, campaignRoot, query, ResolveOptions{})
	if err != nil || wi == nil {
		return ""
	}
	ref, err := workitem.EnsureRefForCommit(ctx, campaignRoot, wi, errw)
	if err != nil {
		return workitem.RefOf(wi)
	}
	return ref
}
