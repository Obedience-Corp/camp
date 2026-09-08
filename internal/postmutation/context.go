// Package postmutation carries the context rule for work that runs after a
// command has already changed the tree.
package postmutation

import (
	"context"
	"time"
)

// Budget bounds the detached context. It is long enough that the reference
// rewrite over a large camp on a contended filesystem finishes, and short
// enough that a wedged rewrite cannot hold a terminal forever.
const Budget = 2 * time.Minute

// Context returns a context for work that must finish even though the caller
// stopped waiting, together with its cancel func. The caller must defer cancel.
//
// The rule it encodes: once a command has renamed a directory, the bookkeeping
// that makes the rename coherent (rewriting the references that pointed at the
// old path, appending the audit line, committing) is not optional follow-up.
// Abandoning it half way leaves the tree changed with nothing recording that it
// happened, which is worse than finishing work the caller walked away from.
//
// It is bounded rather than open-ended. context.WithoutCancel alone makes every
// ctx.Err() check downstream dead code, so a rewrite that wedges on a slow
// filesystem could not be stopped at all. The deadline keeps those checks
// meaningful while still surviving the caller's own cancellation.
func Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), Budget)
}
