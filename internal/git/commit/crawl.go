package commit

import "context"

// CrawlOptions configures a dungeon crawl commit.
type CrawlOptions struct {
	Options
	Description string   // Body text describing moved items
	Files       []string // Paths to stage (e.g., parent dir + dungeon dir)
}

// Crawl stages changes and commits for a dungeon crawl operation.
// If Files is set, only those paths are staged instead of everything.
// SelectiveOnly is automatically enabled when Files is provided.
func Crawl(ctx context.Context, opts CrawlOptions) Result {
	opts.Options.Files = opts.Files
	if opts.Files != nil {
		opts.Options.SelectiveOnly = true
	}
	return doCommit(ctx, opts.Options, "Crawl", "dungeon crawl completed", opts.Description)
}
