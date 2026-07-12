package workitem

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
)

// appendWorkitemAuditEvent records e in the campaign-wide workitem ledger. A
// ledger write failure is best-effort: it must never fail the user's command,
// matching the invalidateNavigationCache pattern for post-write bookkeeping,
// but it is reported on stderr rather than swallowed. The helper deliberately
// leaves the tracked ledger unstaged; the caller's next explicit campaign
// commit picks it up alongside the workitem mutation.
func appendWorkitemAuditEvent(ctx context.Context, cmd *cobra.Command, campaignRoot string, e wkaudit.Event) {
	if err := wkaudit.AppendEvent(ctx, campaignRoot, e); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to append workitem audit event: %v\n", err)
	}
}
