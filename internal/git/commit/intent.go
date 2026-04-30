package commit

import "context"

// IntentAction represents the type of intent operation performed.
type IntentAction string

const (
	IntentCreate  IntentAction = "Create"
	IntentEdit    IntentAction = "Edit"
	IntentMove    IntentAction = "Move"
	IntentArchive IntentAction = "Archive"
	IntentDelete  IntentAction = "Delete"
	IntentGather  IntentAction = "Gather"
	IntentPromote IntentAction = "Promote"
	IntentCrawl   IntentAction = "Crawl"
)

// IntentOptions configures an intent commit.
type IntentOptions struct {
	Options
	Action      IntentAction // The action performed
	IntentTitle string       // Title of the affected intent
	Description string       // Optional body text
}

// Intent stages changes and commits for an intent operation.
// If opts.Options.Files is set, only those paths are staged.
func Intent(ctx context.Context, opts IntentOptions) Result {
	return doCommit(ctx, opts.Options, string(opts.Action), opts.IntentTitle, opts.Description)
}
