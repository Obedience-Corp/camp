package workitem

import (
	"context"

	"github.com/spf13/cobra"

	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
)

// appendWorkitemAuditEvent records e in the campaign-wide workitem ledger via
// wkaudit.AppendBestEffort, the single shared code path every workitem
// command uses to write the ledger. A ledger write failure is best-effort:
// it must never fail the user's command, matching the
// invalidateNavigationCache pattern for post-write bookkeeping, but it is
// reported on stderr rather than swallowed. The helper deliberately leaves
// the tracked ledger unstaged; the caller's next explicit campaign commit
// picks it up alongside the workitem mutation.
func appendWorkitemAuditEvent(ctx context.Context, cmd *cobra.Command, campaignRoot string, e wkaudit.Event) {
	wkaudit.AppendBestEffort(ctx, cmd.ErrOrStderr(), campaignRoot, e)
}
