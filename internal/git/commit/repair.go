package commit

import "context"

// RepairOptions configures a campaign repair commit.
type RepairOptions struct {
	Options
	Description string // Body text describing repair changes
}

// Repair stages changes and commits for a campaign repair operation.
// If opts.Options.Files is set, only those paths are staged.
func Repair(ctx context.Context, opts RepairOptions) Result {
	return doCommit(ctx, opts.Options, "Repair", "campaign repair", opts.Description)
}
