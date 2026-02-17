package commit

import "context"

// IntentAction represents the type of intent operation performed.
type IntentAction string

const (
	IntentCreate  IntentAction = "Create"
	IntentMove    IntentAction = "Move"
	IntentArchive IntentAction = "Archive"
	IntentDelete  IntentAction = "Delete"
	IntentGather  IntentAction = "Gather"
	IntentPromote IntentAction = "Promote"
)

// IntentOptions configures an intent commit.
type IntentOptions struct {
	Options
	Action      IntentAction // The action performed
	IntentTitle string       // Title of the affected intent
	Description string       // Optional body text
}

// Intent stages all changes and commits for an intent operation.
func Intent(ctx context.Context, opts IntentOptions) Result {
	return doCommit(ctx, opts.Options, string(opts.Action), opts.IntentTitle, opts.Description)
}
