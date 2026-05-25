package commit

import "context"

// WorkitemAction identifies the high-level operation captured by the commit.
// WorkitemScope is the generic case used by `camp workitem commit`.
// WorkitemEdit is reserved for flows that mutate workitem metadata.
type WorkitemAction string

const (
	WorkitemEdit  WorkitemAction = "WorkitemEdit"
	WorkitemScope WorkitemAction = "WorkitemScope"
)

// WorkitemOptions configures a workitem-scoped commit. The embedded Options
// carries the campaign root, campaign id, file scope, and the QuestID /
// WorkitemRef fields surfaced through the campaign tag.
type WorkitemOptions struct {
	Options
	Action      WorkitemAction
	WorkitemID  string
	WorkitemRef string
	QuestID     string
	Title       string
	Detail      string
}

// Workitem stages workitem-scoped changes and commits with a campaign tag
// that carries the workitem ref (and quest id, when set).
func Workitem(ctx context.Context, opts WorkitemOptions) Result {
	opts.Options.QuestID = opts.QuestID
	opts.Options.WorkitemRef = opts.WorkitemRef
	return doCommit(ctx, opts.Options, string(opts.Action), opts.Title, opts.Detail)
}
