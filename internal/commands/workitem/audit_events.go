package workitem

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	wkaudit "github.com/Obedience-Corp/camp/internal/workitem/audit"
	"github.com/Obedience-Corp/camp/internal/workitem/priority"
)

// appendWorkitemAuditEvent records e in the campaign-wide workitem ledger. A
// ledger write failure is best-effort: it must never fail the user's command,
// matching the invalidateNavigationCache pattern for post-write bookkeeping,
// but it is reported on stderr rather than swallowed.
func appendWorkitemAuditEvent(ctx context.Context, cmd *cobra.Command, campaignRoot string, e wkaudit.Event) {
	if err := wkaudit.AppendEvent(ctx, campaignRoot, e); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to append workitem audit event: %v\n", err)
	}
}

// attentionStageLabel renders an attention stage for the audit ledger,
// spelling out the cleared state instead of leaving it as an empty string.
func attentionStageLabel(stage priority.AttentionStage) string {
	if stage == priority.AttentionNone {
		return "none"
	}
	return string(stage)
}

// groupLabel renders a workitem group for the audit ledger, spelling out the
// cleared state instead of leaving it as an empty string.
func groupLabel(group string) string {
	if group == "" {
		return "none"
	}
	return group
}
